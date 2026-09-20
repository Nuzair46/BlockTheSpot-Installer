using System.Diagnostics;
using System.IO;
using System.Reflection.PortableExecutable;
using System.Security.Principal;
using System.Text;
using BlockTheSpot.Core;

namespace BlockTheSpot.App;

public sealed class WindowsSpotifyPlatform : ISpotifyPlatform
{
    public string SpotifyDirectory { get; } = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData), "Spotify");
    private string SpotifyExe => Path.Combine(SpotifyDirectory, "Spotify.exe");
    // Spotify downloads updates into %LOCALAPPDATA%\Spotify\Update. A read-only file with that name
    // leaves the updater unable to create the folder, so it cannot stage an update over the patch.
    private static string UpdatePath => Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Spotify", "Update");
    public static bool IsAdministrator => new WindowsPrincipal(WindowsIdentity.GetCurrent()).IsInRole(WindowsBuiltInRole.Administrator);

    public InstalledSpotify Inspect()
    {
        if (!File.Exists(SpotifyExe)) return new(null, false);
        var information = FileVersionInfo.GetVersionInfo(SpotifyExe);
        var version = information.ProductVersion?.Split(' ')[0] ?? $"{information.FileMajorPart}.{information.FileMinorPart}.{information.FileBuildPart}.{information.FilePrivatePart}";
        return new(version, File.Exists(Path.Combine(SpotifyDirectory, "blockthespot.dll")));
    }

    public async Task<bool> IsStoreInstalledAsync(CancellationToken token) =>
        (await PowerShellAsync("if (Get-AppxPackage -Name SpotifyAB.SpotifyMusic -ErrorAction Stop) { 'installed' }", token)).Contains("installed", StringComparison.Ordinal);

    public async Task RemoveStoreAsync(CancellationToken token) =>
        await PowerShellAsync("Get-AppxPackage -Name SpotifyAB.SpotifyMusic -ErrorAction Stop | Remove-AppxPackage -ErrorAction Stop", token);

    public Task StopSpotifyAsync(CancellationToken token) => Task.Run(() =>
    {
        foreach (var name in new[] { "Spotify", "SpotifyWebHelper" })
            foreach (var process in Process.GetProcessesByName(name))
                using (process)
                {
                    token.ThrowIfCancellationRequested();
                    try
                    {
                        // Other users' Spotify processes must not be affected.
                        if (process.SessionId != Process.GetCurrentProcess().SessionId) continue;
                        process.Kill(true);
                        if (!process.WaitForExit(10000)) throw new IOException("Spotify is still running. Close it and try again.");
                    }
                    catch (InvalidOperationException) { /* Process already exited. */ }
                }
    }, token);

    public async Task VerifySpotifyPublisherAsync(string installerPath, CancellationToken token)
    {
        var escaped = installerPath.Replace("'", "''", StringComparison.Ordinal);
        await PowerShellAsync(
            $"$s = Get-AuthenticodeSignature -LiteralPath '{escaped}'; " +
            "if ($s.Status -ne 'Valid' -or $s.SignerCertificate.Subject -notmatch '(?:^|,\\s*)O=Spotify (?:AB|USA Inc\\.)(?:,|$)') " +
            "{ throw 'The installer does not have a valid Spotify digital signature. No setup was run.' }", token);
    }

    public async Task RunSetupAsync(string installerPath, string minimum, SpotifyChoice selected, CancellationToken token)
    {
        if (IsAdministrator) throw new InvalidOperationException("Close this app and open it normally, without 'Run as administrator'. Spotify installs for your Windows account.");
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(token);
        timeout.CancelAfter(TimeSpan.FromMinutes(6));
        using var process = Process.Start(new ProcessStartInfo(installerPath) { UseShellExecute = false })
            ?? throw new InvalidOperationException("Could not start Spotify setup.");
        try { await process.WaitForExitAsync(timeout.Token); }
        catch (OperationCanceledException)
        {
            throw new TimeoutException("Spotify setup has not finished after six minutes. Let setup finish, then reopen this installer.");
        }
        if (process.ExitCode != 0) throw new InvalidOperationException($"Spotify setup exited with code {process.ExitCode}. No patch was applied.");
        var deadline = DateTime.UtcNow.AddSeconds(30);
        while (DateTime.UtcNow < deadline)
        {
            var version = Inspect().Version;
            if (version is not null)
            {
                try
                {
                    SpotifyVersions.ValidateInstalled(version, minimum, selected);
                    ValidateInstalledArchitecture();
                    return;
                }
                catch (InvalidOperationException) { }
                catch (IOException) { }
            }
            await Task.Delay(500, token);
        }
        throw new InvalidOperationException("Spotify setup finished without installing the selected version. No patch was applied.");
    }

    public void ValidateInstalledArchitecture()
    {
        using var stream = File.OpenRead(SpotifyExe);
        using var file = new PEReader(stream);
        if (file.PEHeaders.CoffHeader.Machine != Machine.Amd64)
            throw new InvalidOperationException("BlockTheSpot requires Spotify x64. Enable 'Install this Spotify version' to replace this installation.");
    }

    public void SetUpdatesBlocked(bool blocked)
    {
        try
        {
            if (blocked)
            {
                if (Directory.Exists(UpdatePath)) Directory.Delete(UpdatePath, true);
                if (!File.Exists(UpdatePath))
                {
                    Directory.CreateDirectory(Path.GetDirectoryName(UpdatePath)!);
                    File.WriteAllBytes(UpdatePath, []);
                }
                File.SetAttributes(UpdatePath, FileAttributes.ReadOnly);
            }
            else if (File.Exists(UpdatePath))
            {
                File.SetAttributes(UpdatePath, FileAttributes.Normal);
                File.Delete(UpdatePath);
            }
        }
        // Spotify still runs patched without this; never fail an install over the updater.
        catch (Exception error) when (error is IOException or UnauthorizedAccessException) { }
    }

    public void LaunchSpotify() =>
        Process.Start(new ProcessStartInfo(SpotifyExe) { UseShellExecute = true, WorkingDirectory = SpotifyDirectory })?.Dispose();

    private static async Task<string> PowerShellAsync(string script, CancellationToken token)
    {
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(token);
        timeout.CancelAfter(TimeSpan.FromMinutes(2));
        var start = new ProcessStartInfo(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.System), "WindowsPowerShell", "v1.0", "powershell.exe"))
        {
            UseShellExecute = false, CreateNoWindow = true, RedirectStandardOutput = true, RedirectStandardError = true
        };
        foreach (var argument in new[] { "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand",
            Convert.ToBase64String(Encoding.Unicode.GetBytes("$ErrorActionPreference='Stop'; " + script)) })
            start.ArgumentList.Add(argument);
        using var process = Process.Start(start) ?? throw new InvalidOperationException("Could not start Windows PowerShell.");
        var output = process.StandardOutput.ReadToEndAsync(timeout.Token);
        var errors = process.StandardError.ReadToEndAsync(timeout.Token);
        try { await process.WaitForExitAsync(timeout.Token); }
        catch
        {
            if (!process.HasExited) process.Kill(true);
            throw;
        }
        var text = await output;
        var errorText = await errors;
        if (process.ExitCode != 0) throw new InvalidOperationException(errorText.Trim());
        return text.Trim();
    }
}
