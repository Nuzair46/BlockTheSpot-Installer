using System.Net;
using System.Net.Security;
using System.Security.Authentication;
using System.Security.Cryptography.X509Certificates;
using System.Text;
using BlockTheSpot.Core;
using Xunit;

namespace BlockTheSpot.Tests;

public sealed class CatalogTests
{
    private static string Fixture => File.ReadAllText(Path.Combine(AppContext.BaseDirectory, "loadspot_versions.json"));

    [Fact]
    public void LiveSchemaListsEveryReleaseNewestFirstAndSelectsTheTestedVersion()
    {
        var result = SpotifyVersions.Read(Fixture, "1.3.1.234");
        Assert.Equal(6, result.Choices.Count);
        Assert.Equal("1.3.1.234.g59d6bf59", result.Choices[1].FullVersion);
        Assert.Equal("1.2.85.519.g549a528b", result.Choices[^1].FullVersion);
        Assert.Equal("1.3.1.234.g59d6bf59", result.Selected.FullVersion);
        Assert.True(result.Selected.Recommended);
        Assert.Equal(148153000, result.Selected.Size);
        Assert.Equal("16.09.2026", result.Selected.Date);
        Assert.Equal("https://loadspot.amd64fox1.workers.dev/download/spotify_installer-1.3.1.234.g59d6bf59-x64.exe", result.Selected.Url.AbsoluteUri);
        Assert.Null(result.Selected.Mirror);
        Assert.Equal(SpotifyChoice.Latest, result.Choices[0]);
        Assert.Null(result.Warning);
    }

    [Fact]
    public void OlderVersionsStayListedWhenTheTestedVersionIsMissing()
    {
        var result = SpotifyVersions.Read(Fixture, "1.9.0.0");
        Assert.Equal(6, result.Choices.Count);
        Assert.NotNull(result.Warning);
        Assert.Equal(SpotifyChoice.Latest, result.Selected);
        Assert.All(result.Choices.Skip(1), choice => Assert.False(choice.Recommended));
    }

    [Theory]
    [InlineData("http://loadspot.amd64fox1.workers.dev/download/spotify_installer-1.2.93.667.g7b5cc0ce-x64.exe")]
    [InlineData("https://loadspot.amd64fox1.workers.dev/download/spotify_installer-1.2.93.667.g7b5cc0ce-arm64.exe")]
    [InlineData("https://loadspot.amd64fox1.workers.dev.evil.test/download/spotify_installer-1.2.93.667.g7b5cc0ce-x64.exe")]
    [InlineData("https://user@loadspot.amd64fox1.workers.dev/download/spotify_installer-1.2.93.667.g7b5cc0ce-x64.exe")]
    [InlineData("https://upgrade.scdn.co/upgrade/client/win32-x86_64/spotify_installer-1.2.93.667.g7b5cc0ce-4062.exe?fauth=abc&x=1")]
    [InlineData("https://upgrade.scdn.co/upgrade/client/win32-x86_64/spotify_installer-1.2.93.667.g7b5cc0ce-4062.exe?token=abc")]
    [InlineData("https://loadspot.amd64fox1.workers.dev/download/spotify_installer-1.2.93.667.g7b5cc0ce-x64.exe?fauth=abc")]
    [InlineData("https://upgrade.scdn.co/upgrade/client/win32-x86/spotify_installer-1.2.93.667.g7b5cc0ce-4062.exe")]
    [InlineData("https://upgrade.scdn.co/upgrade/client/win32-x86_64/spotify_installer-1.2.93.668.g7b5cc0ce-4062.exe")]
    public void RejectsWrongArchitectureAndUnexpectedDownloadHosts(string url) =>
        Assert.False(SpotifyVersions.IsCatalogDownload(new(url), "1.2.93.667.g7b5cc0ce"));

    [Fact]
    public void LegacyLinksBecomeSpotifyFirstWithTheMirrorFilenameAsFallback()
    {
        var result = SpotifyVersions.Read("""{"1.2.85.519":{"buildType":"Release","fullversion":"1.2.85.519.g549a528b","links":{"win":{"x64":"https://upgrade.scdn.co/upgrade/client/win32-x86_64/spotify_installer-1.2.85.519.g549a528b-4062.exe"}}}}""", "1.2.85.500");
        var choice = result.Choices[1];
        Assert.EndsWith("-4062.exe", choice.Url.AbsoluteUri);
        Assert.True(SpotifyVersions.IsOfficial(choice.Url));
        Assert.Equal("https://loadspot.amd64fox1.workers.dev/download/spotify_installer-1.2.85.519.g549a528b-x64.exe", choice.Mirror!.AbsoluteUri);
        Assert.Equal(2, choice.Urls.Count());
        Assert.False(choice.Recommended);
        Assert.Equal(SpotifyChoice.Latest, result.Selected);
    }

    [Fact]
    public void PagesFeedWithBothLinksTriesSpotifyThenTheMirror()
    {
        var result = SpotifyVersions.Read("""{"1.2.85.519":{"fullversion":"1.2.85.519.g549a528b","win":{"x64":{"url":"https://loadspot.amd64fox1.workers.dev/download/spotify_installer-1.2.85.519.g549a528b-x64.exe","official":"https://upgrade.scdn.co/upgrade/client/win32-x86_64/spotify_installer-1.2.85.519.g549a528b-4062.exe","date":"12.05.2026","size":10}}}}""", "1.2.93.667");
        var choice = result.Choices[1];
        Assert.Equal("upgrade.scdn.co", choice.Url.Host);
        Assert.Equal("loadspot.amd64fox1.workers.dev", choice.Mirror!.Host);
        Assert.Equal(2, choice.Urls.Count());
        Assert.Equal("Spotify", choice.Source);
        Assert.Equal("12.05.2026", choice.Date);
    }

    [Fact]
    public void WatcherFieldsPinThePermanentLinkByETagAndAddTheArchiveAndHash()
    {
        const string permanent = "https://download.scdn.co/SpotifyFullSetupX64.exe";
        const string archive = "https://github.com/RobyRew/BlockTheSpot-Installer/releases/download/spotify-installers/spotify_installer-1.3.1.234.g59d6bf59-x64.exe";
        var json = "{\"1.3.1.234\":{\"fullversion\":\"1.3.1.234.g59d6bf59\",\"win\":{\"x64\":{\"url\":\"" + permanent + "\",\"etag\":\"\\\"b67d\\\"\",\"archive\":\"" + archive +
            "\",\"sha256\":\"6C25D92DD38DDBFC1E4970765E865766D8AE2EF1758C6AF8E9122C5050EE66DC\",\"date\":\"17.09.2026\",\"size\":148153000}}}}";
        var choice = SpotifyVersions.Read(json, "1.2.93.667").Choices[1];
        Assert.Equal(Sources.LatestSpotify, choice.Url);
        Assert.Equal("\"b67d\"", choice.ETag);
        Assert.Equal(archive, choice.Archive!.AbsoluteUri);
        Assert.Equal("6c25d92dd38ddbfc1e4970765e865766d8ae2ef1758c6af8e9122c5050ee66dc", choice.Sha256);
        Assert.Equal(["download.scdn.co", "github.com", "loadspot.amd64fox1.workers.dev"], choice.Urls.Select(u => u.Host));
        Assert.Equal("Spotify", choice.Source);
        Assert.Contains("SHA-256", choice.Detail);
        Assert.True(choice.Recommended == false);
    }

    [Fact]
    public void PermanentLinkWithoutAnETagIsNotPinnedToAVersion()
    {
        var json = """{"1.3.1.234":{"fullversion":"1.3.1.234.g59d6bf59","win":{"x64":{"url":"https://download.scdn.co/SpotifyFullSetupX64.exe","size":1}}}}""";
        var result = SpotifyVersions.Read(json, "1.2.93.667");
        Assert.Single(result.Choices);
        var withArchive = SpotifyVersions.Read("""{"1.3.1.234":{"fullversion":"1.3.1.234.g59d6bf59","win":{"x64":{"url":"https://download.scdn.co/SpotifyFullSetupX64.exe","archive":"https://github.com/RobyRew/BlockTheSpot-Installer/releases/download/spotify-installers/spotify_installer-1.3.1.234.g59d6bf59-x64.exe","sha256":"bad"}}}}""", "1.2.93.667").Choices[1];
        Assert.Equal("github.com", withArchive.Url.Host);
        Assert.Null(withArchive.ETag);
        Assert.Null(withArchive.Sha256);
        Assert.Equal("GitHub archive", withArchive.Source);
    }

    [Theory]
    [InlineData("https://github.com/RobyRew/BlockTheSpot-Installer/releases/download/spotify-installers/spotify_installer-1.3.1.234.g59d6bf59-x64.exe", "1.3.1.234.g59d6bf59")]
    [InlineData("https://github.com/RobyRew/BlockTheSpot-Installer/releases/download/v9/spotify_installer-1.3.1.235.g59d6bf59-x64.exe", "1.3.1.235.g59d6bf59")]
    [InlineData("https://github.com/Someone/BlockTheSpot-Installer/releases/download/spotify-installers/spotify_installer-1.3.1.234.g59d6bf59-x64.exe", null)]
    [InlineData("https://github.com/RobyRew/BlockTheSpot-Installer/releases/download/spotify-installers/spotify_installer-1.3.1.234.g59d6bf59-arm64.exe", null)]
    [InlineData("http://github.com/RobyRew/BlockTheSpot-Installer/releases/download/spotify-installers/spotify_installer-1.3.1.234.g59d6bf59-x64.exe", null)]
    public void ArchiveLinksMustNameThisRepositoryAndTheExactBuild(string url, string? version)
    {
        Assert.Equal(version == "1.3.1.234.g59d6bf59", SpotifyVersions.IsArchive(new(url), "1.3.1.234.g59d6bf59"));
        var custom = SpotifyVersions.TryCustom(url);
        Assert.Equal(version, custom?.FullVersion);
        if (custom is not null) Assert.Equal(["github.com", "loadspot.amd64fox1.workers.dev"], custom.Urls.Select(u => u.Host));
    }

    [Theory]
    [InlineData("1.2.80.699.gd5f6ebe3", "https://loadspot.amd64fox1.workers.dev/download/spotify_installer-1.2.80.699.gd5f6ebe3-x64.exe", 1)]
    [InlineData("  https://loadspot.amd64fox1.workers.dev/download/spotify_installer-1.2.80.699.gd5f6ebe3-x64.exe ", "https://loadspot.amd64fox1.workers.dev/download/spotify_installer-1.2.80.699.gd5f6ebe3-x64.exe", 1)]
    [InlineData("https://upgrade.scdn.co/upgrade/client/win32-x86_64/spotify_installer-1.2.80.699.gd5f6ebe3-77.exe", "https://upgrade.scdn.co/upgrade/client/win32-x86_64/spotify_installer-1.2.80.699.gd5f6ebe3-77.exe", 2)]
    public void TypedVersionsAndKnownHostLinksBecomeCustomChoices(string input, string url, int sources)
    {
        var choice = SpotifyVersions.TryCustom(input)!;
        Assert.Equal("1.2.80.699.gd5f6ebe3", choice.FullVersion);
        Assert.Equal(url, choice.Url.AbsoluteUri);
        Assert.Equal(sources, choice.Urls.Count());
        Assert.True(choice.Custom);
        Assert.Equal("Custom", choice.Badge);
    }

    [Theory]
    [InlineData("")]
    [InlineData("1.2.80")]
    [InlineData("1.2.80.699")]
    [InlineData("2.0.0.1.gabcdef12")]
    [InlineData("https://example.test/spotify_installer-1.2.80.699.gd5f6ebe3-x64.exe")]
    [InlineData("http://loadspot.amd64fox1.workers.dev/download/spotify_installer-1.2.80.699.gd5f6ebe3-x64.exe")]
    [InlineData("https://loadspot.amd64fox1.workers.dev/download/spotify_installer-1.2.80.699.gd5f6ebe3-arm64.exe")]
    public void PartialVersionsAndForeignLinksAreNotInstallable(string input) => Assert.Null(SpotifyVersions.TryCustom(input));

    [Fact]
    public void SignedSpotifyLinksAreOfficialAndTypedOnesBecomeChoices()
    {
        var signed = new Uri("https://upgrade.scdn.co/upgrade/client/win32-x86_64/spotify_installer-1.3.1.234.g59d6bf59-5377.exe?fauth=eyJr.eyJp.sig-1_2~3");
        Assert.True(SpotifyVersions.IsOfficial(signed));
        Assert.True(SpotifyVersions.IsCatalogDownload(signed, "1.3.1.234.g59d6bf59"));
        var choice = SpotifyVersions.TryCustom(signed.AbsoluteUri)!;
        Assert.Equal(signed, choice.Url);
        Assert.Equal("Spotify", choice.Source);
        var fed = SpotifyVersions.Read("""{"1.3.1.234":{"fullversion":"1.3.1.234.g59d6bf59","win":{"x64":{"url":"https://loadspot.amd64fox1.workers.dev/download/spotify_installer-1.3.1.234.g59d6bf59-x64.exe","official":"https://upgrade.scdn.co/upgrade/client/win32-x86_64/spotify_installer-1.3.1.234.g59d6bf59-5377.exe?fauth=a.b.c"}}}}""", "1.2.93.667").Choices[1];
        Assert.Equal("?fauth=a.b.c", fed.Url.Query);
        Assert.Equal(["upgrade.scdn.co", "loadspot.amd64fox1.workers.dev"], fed.Urls.Select(u => u.Host));
    }

    [Fact]
    public void TheLatestOfficialLinkIsRecognizedAsSpotify()
    {
        Assert.Same(SpotifyChoice.Latest, SpotifyVersions.TryCustom(Sources.LatestSpotify.AbsoluteUri));
        Assert.True(SpotifyVersions.IsOfficial(Sources.LatestSpotify));
        Assert.Equal("Spotify", SpotifyChoice.Latest.Source);
    }

    [Theory]
    [InlineData("<html>error</html>")]
    [InlineData("null")]
    public async Task InvalidCatalogFallsBackWithoutLosingLatest(string catalog)
    {
        using var client = new HttpClient(new FakeHandler(uri => Response.Text(uri == Sources.Config ? ";1.2.93.667" : catalog)));
        var result = await new CatalogService(new Downloads(client)).LoadAsync(CancellationToken.None);
        Assert.NotNull(result.Warning);
        Assert.Equal(Compatibility.TestedChoice, result.Selected);
    }

    [Fact]
    public void NumericVersionComparisonAndConfigParsing()
    {
        Assert.True(SpotifyVersions.Parse("1.2.100.1") > SpotifyVersions.Parse("1.2.99.999"));
        Assert.Equal("1.3.1.223", SpotifyVersions.MinimumFromConfig(";Spotify for Windows\r\n;1.3.1.223\r\n[Log]\r\n"));
        Assert.Throws<InvalidDataException>(() => SpotifyVersions.MinimumFromConfig(";not a version"));
        Assert.Throws<InvalidOperationException>(() => SpotifyVersions.ValidateInstalled("1.2.92.1", "1.2.93.667", null));
        Assert.Throws<InvalidOperationException>(() => SpotifyVersions.ValidateInstalled("1.3.1.223", "1.2.93.667", new("1.2.93.667", Sources.LatestSpotify)));
        SpotifyVersions.ValidateInstalled("1.3.1.223", "1.2.93.667", null);
    }

    [Fact]
    public async Task PagesFeedLoadsWithoutDependingOnOtherServers()
    {
        var handler = new FakeHandler(uri => uri == Sources.CatalogApi ? Response.Text(Fixture) : throw new InvalidOperationException("Unexpected upstream request"));
        using var client = new HttpClient(handler);
        var result = await new CatalogService(new Downloads(client)).LoadAsync(CancellationToken.None);
        Assert.Null(result.Warning);
        Assert.Equal(Compatibility.TestedVersion, result.Selected.FullVersion);
        Assert.Equal([Sources.CatalogApi], handler.Requests);
    }

    [Theory]
    [InlineData("{}")]
    [InlineData("not json")]
    [InlineData("{\"1.2.93.667\":{\"fullversion\":\"1.99999999999999.3.4.gabcdefab\"}}")]
    public async Task BrokenPagesFeedFallsBackToMaintainedUpstream(string bad)
    {
        var handler = new FakeHandler(uri => Response.Text(uri == Sources.CatalogApi ? bad : Fixture));
        using var client = new HttpClient(handler);
        var result = await new CatalogService(new Downloads(client)).LoadAsync(CancellationToken.None);
        Assert.Null(result.Warning);
        Assert.Equal(Compatibility.TestedVersion, result.Selected.FullVersion);
        Assert.Equal([Sources.CatalogApi, Sources.Catalog], handler.Requests);
    }

    [Fact]
    public async Task CallerCancellationDoesNotStartAnUpstreamFallback()
    {
        using var token = new CancellationTokenSource();
        token.Cancel();
        using var client = new HttpClient(new FakeHandler(_ => throw new OperationCanceledException(token.Token)));
        await Assert.ThrowsAnyAsync<OperationCanceledException>(() => new CatalogService(new Downloads(client)).LoadAsync(token.Token));
    }
}

public sealed class TrustTests
{
    private static X509Certificate2Collection Chain()
    {
        var chain = new X509Certificate2Collection();
        chain.ImportFromPemFile(Path.Combine(AppContext.BaseDirectory, "github_com_chain_2026-09-18.pem"));
        return chain;
    }

    [Fact]
    public void BundledRootsAreSelfSignedAuthoritiesCoveringEveryHostTheAppUses()
    {
        var names = Downloads.BundledRoots.Select(root => root.GetNameInfo(X509NameType.SimpleName, false)).ToList();
        Assert.Equal(13, Downloads.BundledRoots.Count);
        foreach (var expected in new[] { "Sectigo Public Server Authentication Root E46", "USERTrust ECC Certification Authority", "ISRG Root X1", "GlobalSign Root CA", "GTS Root R4", "DigiCert Global Root G2" })
            Assert.Contains(expected, names);
        foreach (var root in Downloads.BundledRoots)
        {
            Assert.Equal(root.Subject, root.Issuer);
            Assert.True(root.NotAfter > new DateTime(2028, 1, 1));
            Assert.Contains(root.Extensions.OfType<X509BasicConstraintsExtension>(), e => e.CertificateAuthority);
        }
    }

    [Fact]
    public void GithubChainValidatesAgainstBundledRootsWithoutTheSystemStore()
    {
        var chain = Chain();
        var when = new DateTime(2026, 9, 18, 12, 0, 0, DateTimeKind.Utc);
        Assert.Equal("github.com", chain[0].GetNameInfo(X509NameType.DnsName, false));
        Assert.True(Downloads.ChainsToBundledRoot(chain[0], chain.Skip(1), when));
        Assert.False(Downloads.ChainsToBundledRoot(chain[0], [], when), "without the server's intermediates the leaf cannot reach a root");
        Assert.False(Downloads.ChainsToBundledRoot(chain[0], chain.Skip(1), new DateTime(2027, 6, 1)), "an expired leaf is not rescued");
    }

    [Fact]
    public void OnlyMissingTrustIsRescuedAndEveryOtherFailureNamesTheHost()
    {
        var chain = Chain();
        using var system = new X509Chain();
        system.ChainPolicy.TrustMode = X509ChainTrustMode.CustomRootTrust; // an empty trust store: what a Windows without the root sees
        system.ChainPolicy.RevocationMode = X509RevocationMode.NoCheck;
        system.ChainPolicy.DisableCertificateDownloads = true;
        system.ChainPolicy.VerificationTime = new DateTime(2026, 9, 18, 12, 0, 0, DateTimeKind.Utc);
        foreach (var intermediate in chain.Skip(1)) system.ChainPolicy.ExtraStore.Add(intermediate);
        Assert.False(system.Build(chain[0]));
        Assert.Contains(system.ChainStatus, s => s.Status.HasFlag(X509ChainStatusFlags.UntrustedRoot) || s.Status.HasFlag(X509ChainStatusFlags.PartialChain));
        Assert.True(Downloads.ValidateCertificate(new object(), chain[0], system, SslPolicyErrors.RemoteCertificateChainErrors));
        var mismatch = Assert.Throws<AuthenticationException>(() => Downloads.ValidateCertificate(new object(), chain[0], system, SslPolicyErrors.RemoteCertificateNameMismatch));
        Assert.Contains("not valid for it", mismatch.Message);
        Assert.True(Downloads.ValidateCertificate(new object(), null, null, SslPolicyErrors.None));
        Assert.Throws<AuthenticationException>(() => Downloads.ValidateCertificate(new object(), null, null, SslPolicyErrors.RemoteCertificateChainErrors));
    }

    [Fact]
    public async Task ATrustFailureIsReportedOnceWithoutRetries()
    {
        var handler = new FakeHandler(_ => throw new HttpRequestException("The SSL connection could not be established, see inner exception.",
            new AuthenticationException("Windows on this PC does not trust the root certificate behind github.com (UntrustedRoot)")));
        using var client = new HttpClient(handler);
        var error = await Assert.ThrowsAsync<HttpRequestException>(() => new Downloads(client).TextAsync(Sources.Config, CancellationToken.None));
        Assert.Contains("Secure connection to github.com refused: Windows on this PC does not trust", error.Message);
        Assert.Single(handler.Requests);
    }
}

public sealed class DownloadTests
{
    [Fact]
    public async Task StreamsAndValidatesExecutableThenReplacesTarget()
    {
        using var directory = new TemporaryDirectory();
        var payload = Response.Executable();
        using var client = new HttpClient(new FakeHandler(_ => Response.Binary(payload)));
        var target = Path.Combine(directory.Path, "setup.exe");
        await new Downloads(client).FileAsync(Sources.LatestSpotify, target, payload.Length, false, null, CancellationToken.None);
        Assert.Equal(payload, File.ReadAllBytes(target));
        Assert.Single(Directory.GetFiles(directory.Path));
    }

    [Fact]
    public async Task ChecksumIsVerifiedOverTheWholeStreamAndSentWithIfMatch()
    {
        using var directory = new TemporaryDirectory();
        var payload = Response.Executable();
        var expected = Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(payload)).ToLowerInvariant();
        string? ifMatch = null;
        var handler = new FakeHandler(_ => Response.Binary(payload)) { Inspect = request => ifMatch = request.Headers.IfMatch.ToString() };
        using var client = new HttpClient(handler);
        var target = Path.Combine(directory.Path, "setup.exe");
        await new Downloads(client).FileAsync(Sources.LatestSpotify, target, 0, false, null, CancellationToken.None, expected, "\"b67d\"");
        Assert.Equal("\"b67d\"", ifMatch);
        Assert.True(File.Exists(target));
        var error = await Assert.ThrowsAsync<InvalidDataException>(() =>
            new Downloads(client).FileAsync(Sources.LatestSpotify, Path.Combine(directory.Path, "other.exe"), 0, false, null, CancellationToken.None, new string('0', 64)));
        Assert.Contains("SHA-256", error.Message);
        Assert.Single(Directory.GetFiles(directory.Path));
    }

    [Fact]
    public async Task ARotatedPermanentLinkAnswers412AndIsReportedAsANewerBuild()
    {
        using var directory = new TemporaryDirectory();
        var handler = new FakeHandler(_ => new(HttpStatusCode.PreconditionFailed));
        using var client = new HttpClient(handler);
        var error = await Assert.ThrowsAsync<HttpRequestException>(() =>
            new Downloads(client).FileAsync(Sources.LatestSpotify, Path.Combine(directory.Path, "setup.exe"), 0, false, null, CancellationToken.None, null, "\"old\""));
        Assert.Equal(HttpStatusCode.PreconditionFailed, error.StatusCode);
        Assert.Contains("newer build", error.Message);
        Assert.Single(handler.Requests);
    }

    [Theory]
    [InlineData(403)]
    [InlineData(404)]
    [InlineData(410)]
    public async Task MissingPinnedDownloadsReturnUsefulErrorWithoutSubstitution(int status)
    {
        using var directory = new TemporaryDirectory();
        var handler = new FakeHandler(_ => new((HttpStatusCode)status));
        using var client = new HttpClient(handler);
        var error = await Assert.ThrowsAsync<HttpRequestException>(() =>
            new Downloads(client).FileAsync(Sources.LatestSpotify, Path.Combine(directory.Path, "setup.exe"), 0, false, null, CancellationToken.None));
        Assert.Contains("Refresh versions", error.Message);
        Assert.Single(handler.Requests);
        Assert.Empty(Directory.GetFiles(directory.Path));
    }

    [Theory]
    [InlineData("html")]
    [InlineData("empty")]
    [InlineData("size")]
    [InlineData("architecture")]
    public async Task InvalidPayloadPreservesExistingTargetAndCleansTemporaryFiles(string kind)
    {
        using var directory = new TemporaryDirectory();
        var target = Path.Combine(directory.Path, "setup.exe");
        await File.WriteAllTextAsync(target, "original");
        var payload = kind switch { "html" => Encoding.UTF8.GetBytes("<html>error</html>"), "empty" => [], _ => Response.Executable(x64: kind != "architecture") };
        using var client = new HttpClient(new FakeHandler(_ => Response.Binary(payload)));
        await Assert.ThrowsAnyAsync<Exception>(() => new Downloads(client).FileAsync(Sources.LatestSpotify, target, kind == "size" ? 99999 : 0, kind == "architecture", null, CancellationToken.None));
        Assert.Equal("original", File.ReadAllText(target));
        Assert.Single(Directory.GetFiles(directory.Path));
    }

    [Fact]
    public async Task CancellationNeverReplacesAnExistingFile()
    {
        using var directory = new TemporaryDirectory();
        using var cancellation = new CancellationTokenSource();
        using var client = new HttpClient(new FakeHandler(_ => { cancellation.Cancel(); return Response.Binary(Response.Executable()); }));
        var target = Path.Combine(directory.Path, "setup.exe");
        File.WriteAllText(target, "original");
        await Assert.ThrowsAnyAsync<OperationCanceledException>(() => new Downloads(client).FileAsync(Sources.LatestSpotify, target, 0, false, null, cancellation.Token));
        Assert.Equal("original", File.ReadAllText(target));
        Assert.Single(Directory.GetFiles(directory.Path));
    }
}

public sealed class TransactionTests
{
    [Fact]
    public void PatchAndRestorePreserveOriginalDll()
    {
        using var directory = new TemporaryDirectory();
        var spotify = directory.Create("spotify");
        var staged = Stage(directory);
        File.WriteAllText(Path.Combine(spotify, "chrome_elf.dll"), "original");
        var transaction = new PatchTransaction();
        transaction.Apply(spotify, staged, false);
        Assert.Equal("original", File.ReadAllText(Path.Combine(spotify, "chrome_elf_required.dll")));
        transaction.Apply(spotify, staged, false);
        transaction.Restore(spotify);
        Assert.Equal("original", File.ReadAllText(Path.Combine(spotify, "chrome_elf.dll")));
        Assert.False(File.Exists(Path.Combine(spotify, "blockthespot.dll")));
    }

    [Fact]
    public void FailedPatchRestoresEveryPreviousFile()
    {
        using var directory = new TemporaryDirectory();
        var spotify = directory.Create("spotify");
        File.WriteAllText(Path.Combine(spotify, "chrome_elf.dll"), "original");
        File.WriteAllText(Path.Combine(spotify, "config.ini"), "user settings");
        var transaction = new PatchTransaction(new FailingFiles());
        Assert.Throws<IOException>(() => transaction.Apply(spotify, Stage(directory), false));
        Assert.Equal("original", File.ReadAllText(Path.Combine(spotify, "chrome_elf.dll")));
        Assert.Equal("user settings", File.ReadAllText(Path.Combine(spotify, "config.ini")));
        Assert.False(File.Exists(Path.Combine(spotify, "chrome_elf_required.dll")));
        Assert.False(File.Exists(Path.Combine(spotify, "blockthespot.dll")));
    }

    [Fact]
    public void ReinstallRefreshesBackupInsteadOfKeepingPreviousVersion()
    {
        using var directory = new TemporaryDirectory();
        var spotify = directory.Create("spotify");
        File.WriteAllText(Path.Combine(spotify, "chrome_elf.dll"), "new Spotify original");
        File.WriteAllText(Path.Combine(spotify, "chrome_elf_required.dll"), "stale original");
        new PatchTransaction().Apply(spotify, Stage(directory), true);
        Assert.Equal("new Spotify original", File.ReadAllText(Path.Combine(spotify, "chrome_elf_required.dll")));
    }

    [Fact]
    public void MissingStagedFileDoesNotTouchSpotify()
    {
        using var directory = new TemporaryDirectory();
        var spotify = directory.Create("spotify");
        File.WriteAllText(Path.Combine(spotify, "chrome_elf.dll"), "original");
        Assert.Throws<FileNotFoundException>(() => new PatchTransaction().Apply(spotify, directory.Create("empty"), false));
        Assert.Equal("original", File.ReadAllText(Path.Combine(spotify, "chrome_elf.dll")));
    }

    private static string Stage(TemporaryDirectory directory)
    {
        var stage = directory.Create("stage");
        foreach (var file in new[] { "chrome_elf.dll", "blockthespot.dll", "config.ini" })
            File.WriteAllText(Path.Combine(stage, file), "patch " + file);
        return stage;
    }

    private sealed class FailingFiles : IFileOperations
    {
        private int count;
        public void Copy(string source, string destination)
        { if (++count == 3) throw new IOException("Simulated disk failure"); File.Copy(source, destination, true); }
        public void Delete(string path) => File.Delete(path);
    }
}

public sealed class InstallerTests
{
    [Fact]
    public async Task KeepingCurrentVersionDoesNotForceADowngrade()
    {
        using var directory = new TemporaryDirectory();
        var platform = new FakePlatform(directory.Path) { Version = "1.3.1.223" };
        File.WriteAllText(Path.Combine(directory.Path, "chrome_elf.dll"), "original");
        using var client = Client();
        await new InstallerService(new Downloads(client), platform).InstallAsync(
            new(new("1.2.93.667", Sources.LatestSpotify), false, false, false, true), new InlineProgress<InstallProgress>(_ => { }), CancellationToken.None);
        Assert.DoesNotContain("setup", platform.Events);
        Assert.True(File.Exists(Path.Combine(directory.Path, "blockthespot.dll")));
    }

    [Fact]
    public async Task FailedDownloadLeavesSpotifyRunningAndUntouched()
    {
        using var directory = new TemporaryDirectory();
        var platform = new FakePlatform(directory.Path);
        using var client = Client(failPatch: true);
        await Assert.ThrowsAsync<HttpRequestException>(() => new InstallerService(new Downloads(client), platform).InstallAsync(
            new(Compatibility.TestedChoice with { Size = 0 }, true, false, false), new InlineProgress<InstallProgress>(_ => { }), CancellationToken.None));
        Assert.DoesNotContain("stop", platform.Events);
        Assert.DoesNotContain("setup", platform.Events);
        Assert.Empty(Directory.GetFiles(directory.Path));
    }

    [Fact]
    public async Task SignatureIsVerifiedBeforeSpotifyIsStoppedOrSetupRuns()
    {
        using var directory = new TemporaryDirectory();
        var platform = new FakePlatform(directory.Path);
        using var client = Client();
        // The pinned hash belongs to the real tested file; the fake payload is exempted here and pinned in its own test.
        await new InstallerService(new Downloads(client), platform).InstallAsync(
            new(Compatibility.TestedChoice with { Size = 0, Sha256 = null }, true, true, false), new InlineProgress<InstallProgress>(_ => { }), CancellationToken.None);
        Assert.True(platform.Events.IndexOf("signature") < platform.Events.IndexOf("stop"));
        Assert.True(platform.Events.IndexOf("signature") < platform.Events.IndexOf("setup"));
        Assert.Contains("launch", platform.Events);
    }

    [Fact]
    public async Task StoreRemovalRequiresExplicitOption()
    {
        using var directory = new TemporaryDirectory();
        var platform = new FakePlatform(directory.Path) { StoreInstalled = true };
        using var client = Client();
        await Assert.ThrowsAsync<InvalidOperationException>(() => new InstallerService(new Downloads(client), platform).InstallAsync(
            new(Compatibility.TestedChoice with { Size = 0 }, true, false, false), new InlineProgress<InstallProgress>(_ => { }), CancellationToken.None));
        Assert.DoesNotContain("remove-store", platform.Events);
        Assert.DoesNotContain("stop", platform.Events);
    }

    [Fact]
    public async Task ExpiredSpotifyLinkFallsBackToTheMirrorAndReportsIt()
    {
        using var directory = new TemporaryDirectory();
        var platform = new FakePlatform(directory.Path);
        var official = new Uri("https://upgrade.scdn.co/upgrade/client/win32-x86_64/spotify_installer-1.2.93.667.g7b5cc0ce-4062.exe");
        var handler = new FakeHandler(uri =>
            uri == Sources.Config ? Response.Text(";1.2.93.667") :
            uri == official ? new(HttpStatusCode.Forbidden) : Response.Binary(Response.Executable()));
        using var client = new HttpClient(handler);
        var stages = new List<string>();
        await new InstallerService(new Downloads(client), platform).InstallAsync(
            new(Compatibility.TestedChoice with { Url = official, Mirror = Compatibility.TestedChoice.Url, Size = 0, Sha256 = null }, true, false, false),
            new InlineProgress<InstallProgress>(p => stages.Add(p.Stage + ": " + p.Detail)), CancellationToken.None);
        Assert.Contains(official, handler.Requests);
        Assert.Contains(Compatibility.TestedChoice.Url, handler.Requests);
        Assert.Contains(stages, s => s.StartsWith("Switching source: Spotify did not serve this version (HTTP 403)"));
        Assert.Contains("setup", platform.Events);
    }

    [Fact]
    public async Task ARotatedPermanentLinkFallsBackToTheArchiveWhoseHashMustStillMatch()
    {
        using var directory = new TemporaryDirectory();
        var platform = new FakePlatform(directory.Path);
        var payload = Response.Executable();
        var sha256 = Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(payload)).ToLowerInvariant();
        var archive = new Uri("https://github.com/RobyRew/BlockTheSpot-Installer/releases/download/spotify-installers/spotify_installer-1.2.93.667.g7b5cc0ce-x64.exe");
        var handler = new FakeHandler(uri =>
            uri == Sources.Config ? Response.Text(";1.2.93.667") :
            uri == Sources.LatestSpotify ? new(HttpStatusCode.PreconditionFailed) : Response.Binary(payload));
        using var client = new HttpClient(handler);
        var stages = new List<string>();
        var choice = Compatibility.TestedChoice with { Url = Sources.LatestSpotify, ETag = "\"old\"", Archive = archive, Mirror = Compatibility.TestedChoice.Url, Size = 0, Sha256 = sha256 };
        await new InstallerService(new Downloads(client), platform).InstallAsync(
            new(choice, true, false, false), new InlineProgress<InstallProgress>(p => stages.Add(p.Stage + ": " + p.Detail)), CancellationToken.None);
        Assert.Equal([Sources.LatestSpotify, archive], handler.Requests.Where(u => u.Host != "github.com" || u == archive));
        Assert.Contains(stages, s => s.StartsWith("Switching source: Spotify did not serve this version (HTTP 412). Trying GitHub archive."));
        Assert.Contains("setup", platform.Events);

        var tampered = new FakePlatform(directory.Path);
        var wrong = choice with { Sha256 = new string('f', 64) };
        await Assert.ThrowsAsync<InvalidDataException>(() => new InstallerService(new Downloads(client), tampered).InstallAsync(
            new(wrong, true, false, false), new InlineProgress<InstallProgress>(_ => { }), CancellationToken.None));
        Assert.DoesNotContain("setup", tampered.Events);
        Assert.DoesNotContain(Compatibility.TestedChoice.Url, handler.Requests);
    }

    [Fact]
    public async Task AMirrorOnlyChoiceIsNotRetriedElsewhere()
    {
        using var directory = new TemporaryDirectory();
        var platform = new FakePlatform(directory.Path);
        var handler = new FakeHandler(uri => uri == Sources.Config ? Response.Text(";1.2.93.667") :
            uri.Host == "github.com" ? Response.Binary(Response.Executable()) : new(HttpStatusCode.NotFound));
        using var client = new HttpClient(handler);
        await Assert.ThrowsAsync<HttpRequestException>(() => new InstallerService(new Downloads(client), platform).InstallAsync(
            new(Compatibility.TestedChoice with { Size = 0 }, true, false, false), new InlineProgress<InstallProgress>(_ => { }), CancellationToken.None));
        Assert.DoesNotContain("setup", platform.Events);
        Assert.Equal([Compatibility.TestedChoice.Url], handler.Requests.Where(uri => uri.Host != "github.com"));
    }

    [Fact]
    public async Task AMismatchedSpotifyFileIsRejectedInsteadOfSubstitutedFromTheMirror()
    {
        using var directory = new TemporaryDirectory();
        var platform = new FakePlatform(directory.Path);
        var official = new Uri("https://upgrade.scdn.co/upgrade/client/win32-x86_64/spotify_installer-1.2.93.667.g7b5cc0ce-1.exe");
        var handler = new FakeHandler(uri => uri == Sources.Config ? Response.Text(";1.2.93.667") : Response.Binary(Response.Executable()));
        using var client = new HttpClient(handler);
        await Assert.ThrowsAsync<InvalidDataException>(() => new InstallerService(new Downloads(client), platform).InstallAsync(
            new(Compatibility.TestedChoice with { Url = official, Mirror = Compatibility.TestedChoice.Url, Size = 999 }, true, false, false),
            new InlineProgress<InstallProgress>(_ => { }), CancellationToken.None));
        Assert.DoesNotContain(Compatibility.TestedChoice.Url, handler.Requests);
        Assert.DoesNotContain("setup", platform.Events);
    }

    [Fact]
    public async Task SpotifyOnlyInstallSkipsThePatchServerAndAcceptsAnyVersion()
    {
        using var directory = new TemporaryDirectory();
        var platform = new FakePlatform(directory.Path) { Version = "1.2.93.667" };
        File.WriteAllText(Path.Combine(directory.Path, "blockthespot.dll"), "old patch");
        File.WriteAllText(Path.Combine(directory.Path, "chrome_elf_required.dll"), "old backup");
        var handler = new FakeHandler(uri => uri.Host == "github.com" ? throw new InvalidOperationException("Patch server contacted") : Response.Binary(Response.Executable()));
        using var client = new HttpClient(handler);
        var choice = SpotifyVersions.TryCustom("1.2.40.599.g606b7f29")!;
        await new InstallerService(new Downloads(client), platform).InstallAsync(
            new(choice, true, false, false, AllowUntested: false, ApplyPatch: false), new InlineProgress<InstallProgress>(_ => { }), CancellationToken.None);
        // Turning the patch off also releases the updater lock, so Spotify can update again.
        Assert.Equal(["signature", "stop", "setup", "stop", "allow-updates"], platform.Events);
        Assert.Equal("1.2.40.599.g606b7f29", platform.Version);
        Assert.Equal(["chrome_elf.dll"], Directory.GetFiles(directory.Path).Select(Path.GetFileName).Order());
    }

    [Fact]
    public async Task SpotifyOnlyWithoutReinstallHasNothingToDo()
    {
        using var directory = new TemporaryDirectory();
        var platform = new FakePlatform(directory.Path) { Version = "1.2.93.667" };
        using var client = Client();
        await Assert.ThrowsAsync<InvalidOperationException>(() => new InstallerService(new Downloads(client), platform).InstallAsync(
            new(Compatibility.TestedChoice, false, false, false, ApplyPatch: false), new InlineProgress<InstallProgress>(_ => { }), CancellationToken.None));
        Assert.Empty(platform.Events);
    }

    [Fact]
    public async Task PatchRefusesVersionsBelowItsMinimumAndPointsToSpotifyOnlyMode()
    {
        using var directory = new TemporaryDirectory();
        var platform = new FakePlatform(directory.Path);
        using var client = Client();
        var error = await Assert.ThrowsAsync<InvalidOperationException>(() => new InstallerService(new Downloads(client), platform).InstallAsync(
            new(SpotifyVersions.TryCustom("1.2.40.599.g606b7f29")!, true, false, false, AllowUntested: true), new InlineProgress<InstallProgress>(_ => { }), CancellationToken.None));
        Assert.Contains("turn off the patch", error.Message);
        Assert.Empty(platform.Events);
    }

    [Fact]
    public async Task PatchingBlocksTheUpdaterAndRestoringReleasesIt()
    {
        using var directory = new TemporaryDirectory();
        var platform = new FakePlatform(directory.Path) { Version = "1.3.1.234.g59d6bf59" };
        File.WriteAllText(Path.Combine(directory.Path, "chrome_elf.dll"), "original");
        using var client = Client();
        var installer = new InstallerService(new Downloads(client), platform);
        await installer.InstallAsync(new(Compatibility.TestedChoice, false, false, false),
            new InlineProgress<InstallProgress>(_ => { }), CancellationToken.None);
        // The updater is stopped only after the files are in place, never before.
        Assert.Contains("block-updates", platform.Events);
        Assert.True(platform.Events.IndexOf("block-updates") > platform.Events.IndexOf("stop"));
        Assert.DoesNotContain("allow-updates", platform.Events);

        platform.Events.Clear();
        await installer.RestoreAsync(new InlineProgress<InstallProgress>(_ => { }), CancellationToken.None);
        Assert.Contains("allow-updates", platform.Events);
        Assert.DoesNotContain("block-updates", platform.Events);
    }

    [Fact]
    public async Task InstalledLegacySpotifyGetsTheLegacyKit()
    {
        using var directory = new TemporaryDirectory();
        var platform = new FakePlatform(directory.Path) { Version = "1.2.93.667" };
        File.WriteAllText(Path.Combine(directory.Path, "chrome_elf.dll"), "original");
        using var client = Client();
        var stages = new List<string>();
        await new InstallerService(new Downloads(client), platform).InstallAsync(
            new(Compatibility.LegacyChoice, false, false, false), new InlineProgress<InstallProgress>(p => stages.Add(p.Detail)), CancellationToken.None);
        var dll = File.ReadAllBytes(Path.Combine(directory.Path, "blockthespot.dll"));
        Assert.Equal(0x75, dll[6372]);
        Assert.Contains("1.2.93.667", File.ReadAllText(Path.Combine(directory.Path, "config.ini")));
        Assert.DoesNotContain("setup", platform.Events);
        Assert.Contains(stages, d => d.Contains("legacy kit"));
    }

    [Fact]
    public async Task InstalledCurrentSpotifyGetsTheCurrentKit()
    {
        using var directory = new TemporaryDirectory();
        var platform = new FakePlatform(directory.Path) { Version = "1.3.1.234.g59d6bf59" };
        File.WriteAllText(Path.Combine(directory.Path, "chrome_elf.dll"), "original");
        using var client = Client();
        await new InstallerService(new Downloads(client), platform).InstallAsync(
            new(Compatibility.TestedChoice, false, false, false), new InlineProgress<InstallProgress>(_ => { }), CancellationToken.None);
        var dll = File.ReadAllBytes(Path.Combine(directory.Path, "blockthespot.dll"));
        Assert.Equal(0x90, dll[6372]);
        Assert.Contains("1.3.1.234", File.ReadAllText(Path.Combine(directory.Path, "config.ini")));
        Assert.DoesNotContain("setup", platform.Events);
    }

    [Fact]
    public async Task ReinstallingAnOlderVersionStagesThatVersionsKit()
    {
        using var directory = new TemporaryDirectory();
        // Spotify 1.3.x is installed, but the legacy build is reinstalled: the patch must follow the file that ends up on disk.
        var platform = new FakePlatform(directory.Path) { Version = "1.3.1.234.g59d6bf59" };
        using var client = Client();
        await new InstallerService(new Downloads(client), platform).InstallAsync(
            new(Compatibility.LegacyChoice with { Sha256 = null, Size = 0 }, true, false, false), new InlineProgress<InstallProgress>(_ => { }), CancellationToken.None);
        Assert.Contains("setup", platform.Events);
        Assert.Equal("1.2.93.667.g7b5cc0ce", platform.Version);
        Assert.Equal(0x75, File.ReadAllBytes(Path.Combine(directory.Path, "blockthespot.dll"))[6372]);
    }

    private static HttpClient Client(bool failPatch = false) => new(new FakeHandler(uri =>
        uri == Sources.Config ? Response.Text(";Spotify for Windows\n;1.2.93.667\n[Log]\nLevel=0") :
        failPatch ? new(HttpStatusCode.NotFound) : Response.Binary(Response.Executable())));

    private sealed class FakePlatform(string directory) : ISpotifyPlatform
    {
        public string SpotifyDirectory => directory;
        public string? Version { get; set; }
        public bool StoreInstalled { get; set; }
        public List<string> Events { get; } = [];
        public InstalledSpotify Inspect() => new(Version, false);
        public void ValidateInstalledArchitecture() { }
        public Task<bool> IsStoreInstalledAsync(CancellationToken token) => Task.FromResult(StoreInstalled);
        public Task RemoveStoreAsync(CancellationToken token) { Events.Add("remove-store"); return Task.CompletedTask; }
        public Task StopSpotifyAsync(CancellationToken token) { Events.Add("stop"); return Task.CompletedTask; }
        public Task VerifySpotifyPublisherAsync(string path, CancellationToken token) { Events.Add("signature"); return Task.CompletedTask; }
        public Task RunSetupAsync(string path, string minimum, SpotifyChoice selected, CancellationToken token)
        {
            Events.Add("setup"); Version = selected.FullVersion ?? "1.3.1.223";
            File.WriteAllText(Path.Combine(directory, "chrome_elf.dll"), "original"); return Task.CompletedTask;
        }
        public void SetUpdatesBlocked(bool blocked) { Events.Add(blocked ? "block-updates" : "allow-updates"); }
        public void LaunchSpotify() => Events.Add("launch");
    }
}

internal sealed class FakeHandler(Func<Uri, HttpResponseMessage> respond) : HttpMessageHandler
{
    public List<Uri> Requests { get; } = [];
    public Action<HttpRequestMessage>? Inspect { get; init; }
    protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken token)
    { Requests.Add(request.RequestUri!); Inspect?.Invoke(request); return Task.FromResult(respond(request.RequestUri!)); }
}

internal static class Response
{
    public static HttpResponseMessage Text(string text) => new(HttpStatusCode.OK) { Content = new StringContent(text) };
    public static HttpResponseMessage Binary(byte[] bytes) => new(HttpStatusCode.OK) { Content = new ByteArrayContent(bytes) };
    public static byte[] Executable(bool x64 = true)
    {
        var bytes = new byte[512];
        using var writer = new BinaryWriter(new MemoryStream(bytes));
        writer.Write((ushort)0x5A4D);
        writer.BaseStream.Position = 0x3C; writer.Write(0x80);
        writer.BaseStream.Position = 0x80; writer.Write(0x00004550);
        writer.Write((ushort)(x64 ? 0x8664 : 0x14c));
        writer.Write((ushort)0); writer.Write(0); writer.Write(0); writer.Write(0);
        writer.Write((ushort)240); writer.Write((ushort)0x2022);
        writer.Write((ushort)0x20B);
        return bytes;
    }
}

internal sealed class TemporaryDirectory : IDisposable
{
    public string Path { get; } = System.IO.Path.Combine(System.IO.Path.GetTempPath(), "BlockTheSpot.Tests-" + Guid.NewGuid().ToString("N"));
    public TemporaryDirectory() => Directory.CreateDirectory(Path);
    public string Create(string name) { var path = System.IO.Path.Combine(Path, name); Directory.CreateDirectory(path); return path; }
    public void Dispose() => Directory.Delete(Path, true);
}
