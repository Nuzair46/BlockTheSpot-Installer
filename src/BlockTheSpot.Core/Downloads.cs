using System.Buffers;
using System.Net;
using System.Net.Http.Headers;
using System.Net.Security;
using System.Reflection.PortableExecutable;
using System.Security.Authentication;
using System.Security.Cryptography;
using System.Security.Cryptography.X509Certificates;

namespace BlockTheSpot.Core;

public sealed record TransferProgress(long Bytes, long? Total)
{
    public double Percent => Total > 0 ? Math.Min(100, 100d * Bytes / Total.Value) : 0;
    public string Label => Total > 0 ? $"{Bytes / 1048576d:F1} / {Total / 1048576d:F1} MiB" : $"{Bytes / 1048576d:F1} MiB";
}

public sealed class Downloads(HttpClient client)
{
    public static HttpClient CreateClient() => new(new SocketsHttpHandler
    {
        ConnectTimeout = TimeSpan.FromSeconds(20),
        PooledConnectionLifetime = TimeSpan.FromMinutes(5),
        AutomaticDecompression = DecompressionMethods.All,
        SslOptions = { RemoteCertificateValidationCallback = ValidateCertificate }
    }) { Timeout = Timeout.InfiniteTimeSpan, DefaultRequestHeaders = { { "User-Agent", "BlockTheSpotInstaller" } } };

    // Windows without automatic root updates lacks recent public roots; github.com chains to
    // Sectigo's 2021 root and was reported as UntrustedRoot. Public roots for every host the app
    // uses are shipped with it (Roots.pem) and consulted only when the system chain fails for
    // missing trust alone. Name mismatches, expiry or bad signatures still fail.
    public static X509Certificate2Collection BundledRoots { get; } = LoadBundledRoots();

    private static X509Certificate2Collection LoadBundledRoots()
    {
        using var stream = typeof(Downloads).Assembly.GetManifestResourceStream("BlockTheSpot.Core.Roots.pem")
            ?? throw new InvalidOperationException("Bundled root certificates are missing from the build.");
        using var reader = new StreamReader(stream);
        var roots = new X509Certificate2Collection();
        roots.ImportFromPem(reader.ReadToEnd());
        return roots;
    }

    private const X509ChainStatusFlags MissingTrustOnly = X509ChainStatusFlags.UntrustedRoot | X509ChainStatusFlags.PartialChain |
        X509ChainStatusFlags.OfflineRevocation | X509ChainStatusFlags.RevocationStatusUnknown;

    public static bool ValidateCertificate(object sender, X509Certificate? certificate, X509Chain? chain, SslPolicyErrors errors)
    {
        if (errors == SslPolicyErrors.None) return true;
        var host = sender is SslStream stream ? stream.TargetHostName : "the server";
        if (errors != SslPolicyErrors.RemoteCertificateChainErrors || certificate is null || chain is null)
            throw new AuthenticationException($"The certificate presented for {host} is not valid for it ({errors}).");
        var status = string.Join(", ", chain.ChainStatus.Select(s => s.Status.ToString()).Distinct());
        if (chain.ChainStatus.Any(s => (s.Status & ~MissingTrustOnly) != 0))
            throw new AuthenticationException($"The certificate chain of {host} is invalid ({status}).");
        using var leaf = new X509Certificate2(certificate);
        if (ChainsToBundledRoot(leaf, chain.ChainElements.Select(e => e.Certificate)))
            return true;
        var top = chain.ChainElements[^1].Certificate.Issuer;
        throw new AuthenticationException($"Windows on this PC does not trust the root certificate behind {host} ({status}; issuer {top}), " +
            "and it is not one shipped with this app. Install Windows updates so root certificates refresh, check the date and time, " +
            "and check antivirus HTTPS scanning or a company proxy.");
    }

    /// <summary>Rebuilds the chain with the bundled roots as the only trust anchors and the server's intermediates as extras.</summary>
    public static bool ChainsToBundledRoot(X509Certificate2 leaf, IEnumerable<X509Certificate2> intermediates, DateTime? verificationTime = null)
    {
        using var custom = new X509Chain();
        custom.ChainPolicy.TrustMode = X509ChainTrustMode.CustomRootTrust;
        custom.ChainPolicy.CustomTrustStore.AddRange(BundledRoots);
        custom.ChainPolicy.RevocationMode = X509RevocationMode.NoCheck;
        custom.ChainPolicy.DisableCertificateDownloads = true;
        foreach (var intermediate in intermediates) custom.ChainPolicy.ExtraStore.Add(intermediate);
        if (verificationTime is { } time) custom.ChainPolicy.VerificationTime = time;
        return custom.Build(leaf);
    }

    public async Task<string> TextAsync(Uri uri, CancellationToken token)
    {
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(token);
        timeout.CancelAfter(TimeSpan.FromSeconds(30));
        using var response = await SendAsync(uri, timeout.Token);
        await response.Content.LoadIntoBufferAsync(4 * 1024 * 1024, timeout.Token);
        return await response.Content.ReadAsStringAsync(timeout.Token);
    }

    /// <param name="sha256">Expected hash of the whole file; a mismatch rejects the file after the transfer.</param>
    /// <param name="etag">Sent as If-Match so a permanent URL that has moved to a newer build answers 412 instead of a wrong file.</param>
    public async Task FileAsync(Uri uri, string target, long expectedSize, bool requireX64Dll,
        IProgress<TransferProgress>? progress, CancellationToken token, string? sha256 = null, string? etag = null)
    {
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(token);
        timeout.CancelAfter(TimeSpan.FromMinutes(10));
        var cancellation = timeout.Token;
        var temporary = target + "." + Guid.NewGuid().ToString("N") + ".download";
        using var hash = sha256 is null ? null : IncrementalHash.CreateHash(HashAlgorithmName.SHA256);
        try
        {
            using var response = await SendAsync(uri, cancellation, etag);
            if (response.StatusCode != HttpStatusCode.OK)
                throw new HttpRequestException($"Expected a complete file from {uri.Host}; received HTTP {(int)response.StatusCode}.");
            var total = response.Content.Headers.ContentLength;
            if (expectedSize > 0 && total > 0 && total != expectedSize)
                throw new InvalidDataException("The download size has changed. Refresh versions and try again.");
            Directory.CreateDirectory(Path.GetDirectoryName(Path.GetFullPath(target))!);
            await using (var input = await response.Content.ReadAsStreamAsync(cancellation))
            await using (var output = new FileStream(temporary, FileMode.CreateNew, FileAccess.Write, FileShare.None, 131072, true))
            {
                var buffer = ArrayPool<byte>.Shared.Rent(131072);
                try
                {
                    long written = 0;
                    var lastReport = Environment.TickCount64;
                    int count;
                    while ((count = await input.ReadAsync(buffer, cancellation)) > 0)
                    {
                        await output.WriteAsync(buffer.AsMemory(0, count), cancellation);
                        hash?.AppendData(buffer, 0, count);
                        written += count;
                        if (Environment.TickCount64 - lastReport > 100)
                        {
                            progress?.Report(new(written, total ?? (expectedSize > 0 ? expectedSize : null)));
                            lastReport = Environment.TickCount64;
                        }
                    }
                    if (written == 0 || (expectedSize > 0 && written != expectedSize) || (total.HasValue && written != total))
                        throw new InvalidDataException("The download is incomplete. Please try again.");
                    progress?.Report(new(written, total));
                }
                finally { ArrayPool<byte>.Shared.Return(buffer); }
            }
            if (hash is not null && !string.Equals(Convert.ToHexString(hash.GetHashAndReset()), sha256, StringComparison.OrdinalIgnoreCase))
                throw new InvalidDataException("The download does not match the SHA-256 checksum recorded from Spotify's servers. Refresh versions and try again.");
            ValidatePortableExecutable(temporary, requireX64Dll);
            cancellation.ThrowIfCancellationRequested();
            File.Move(temporary, target, true);
        }
        finally { if (File.Exists(temporary)) File.Delete(temporary); }
    }

    public static void ValidatePortableExecutable(string path, bool requireX64Dll)
    {
        using var stream = File.OpenRead(path);
        using var executable = new PEReader(stream);
        if (executable.PEHeaders.PEHeader is null ||
            !executable.PEHeaders.CoffHeader.Characteristics.HasFlag(Characteristics.ExecutableImage))
            throw new InvalidDataException("The downloaded file is not a Windows executable.");
        if (requireX64Dll && (executable.PEHeaders.CoffHeader.Machine != Machine.Amd64 ||
            !executable.PEHeaders.CoffHeader.Characteristics.HasFlag(Characteristics.Dll)))
            throw new InvalidDataException("The downloaded patch is not a Windows x64 DLL.");
    }

    private async Task<HttpResponseMessage> SendAsync(Uri uri, CancellationToken token, string? etag = null)
    {
        for (var attempt = 0; ; attempt++)
        {
            HttpResponseMessage response;
            using var request = new HttpRequestMessage(HttpMethod.Get, uri);
            if (etag is not null && EntityTagHeaderValue.TryParse(etag, out var tag)) request.Headers.IfMatch.Add(tag);
            try { response = await client.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, token); }
            catch (HttpRequestException error) when (error.InnerException is AuthenticationException tls)
            {
                // A trust failure is deterministic; retrying only delays the same answer.
                throw new HttpRequestException($"Secure connection to {uri.Host} refused: {tls.Message}", error);
            }
            catch (HttpRequestException) when (attempt < 2)
            {
                await Task.Delay(TimeSpan.FromSeconds(attempt + 1), token);
                continue;
            }
            if (((int)response.StatusCode >= 500 || response.StatusCode == HttpStatusCode.TooManyRequests) && attempt < 2)
            {
                response.Dispose();
                await Task.Delay(TimeSpan.FromSeconds(attempt + 1), token);
                continue;
            }
            if (!response.IsSuccessStatusCode)
            {
                var code = response.StatusCode;
                response.Dispose();
                if (code == HttpStatusCode.PreconditionFailed)
                    throw new HttpRequestException($"{uri.Host} now serves a newer build at this address (HTTP 412). Refresh versions to see it.", null, code);
                var action = code is HttpStatusCode.NotFound or HttpStatusCode.Gone or HttpStatusCode.Forbidden
                    ? " Refresh versions or choose the latest official Spotify installer." : " Please try again.";
                throw new HttpRequestException($"Download unavailable from {uri.Host} (HTTP {(int)code}).{action}", null, code);
            }
            return response;
        }
    }
}
