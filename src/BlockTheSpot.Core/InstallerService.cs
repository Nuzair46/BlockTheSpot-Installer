namespace BlockTheSpot.Core;

public sealed record InstallRequest(SpotifyChoice Choice, bool ReinstallSpotify, bool LaunchSpotify, bool RemoveStoreEdition, bool AllowUntested = false, bool ApplyPatch = true, bool AddPanel = true);
public sealed record InstalledSpotify(string? Version, bool Patched);
public sealed record InstallProgress(string Stage, string Detail, double Percent, bool CanCancel = true);

public interface ISpotifyPlatform
{
    string SpotifyDirectory { get; }
    InstalledSpotify Inspect();
    void ValidateInstalledArchitecture();
    Task<bool> IsStoreInstalledAsync(CancellationToken token);
    Task RemoveStoreAsync(CancellationToken token);
    Task StopSpotifyAsync(CancellationToken token);
    Task VerifySpotifyPublisherAsync(string installerPath, CancellationToken token);
    Task RunSetupAsync(string installerPath, string minimum, SpotifyChoice selected, CancellationToken token);
    /// <summary>Stops or restores Spotify's self-updater. The patch is undone by any update that lands.</summary>
    void SetUpdatesBlocked(bool blocked);
    void LaunchSpotify();
}

public sealed class InstallerService(Downloads downloads, ISpotifyPlatform platform, PatchTransaction? transaction = null)
{
    private readonly PatchTransaction patch = transaction ?? new PatchTransaction();
    /// <summary>Shown in the injected panel so it can name the installer that put it there.</summary>
    public static string AppVersion { get; set; } = typeof(InstallerService).Assembly.GetName().Version?.ToString(3) ?? "dev";
    private readonly SemaphoreSlim gate = new(1, 1);
    // Any version is accepted when only Spotify is installed; the patch has its own floor per channel.
    private const string NoMinimum = "1.0.0.0";

    public async Task InstallAsync(InstallRequest request, IProgress<InstallProgress> progress, CancellationToken token)
    {
        if (!await gate.WaitAsync(0, token)) throw new InvalidOperationException("An installation is already running.");
        var staging = Path.Combine(Path.GetTempPath(), "BlockTheSpot-" + Guid.NewGuid().ToString("N"));
        try
        {
            Directory.CreateDirectory(staging);
            var installed = platform.Inspect();
            // Keeping the installed version is an explicit choice: selecting a catalog
            // entry alone never silently downgrades a working Spotify installation.
            var needsSetup = installed.Version is null || request.ReinstallSpotify;
            if (!needsSetup && !request.ApplyPatch)
                throw new InvalidOperationException("Nothing to do: enable 'Install this Spotify version' or 'Apply BlockTheSpot patch'.");
            var minimum = NoMinimum;
            PatchKit? staged = null;
            if (request.ApplyPatch)
            {
                progress.Report(new("Preparing", "Selecting the BlockTheSpot kit for this Spotify version", 3));
                if (needsSetup) Compatibility.ValidateChoice(request.Choice, request.AllowUntested);
                else Compatibility.ValidateInstalled(installed.Version!, request.AllowUntested);
                // The kit follows the version that will be running when the patch is applied: the
                // selected build when Spotify is (re)installed, the installed build when it is kept.
                var plannedVersion = needsSetup ? request.Choice.FullVersion ?? Compatibility.TestedVersion : installed.Version!;
                var planned = Compatibility.KitFor(plannedVersion)
                    ?? throw new InvalidOperationException($"Spotify {plannedVersion} is older than the {Compatibility.LegacyKit.Label} minimum {Compatibility.LegacyKit.Floor}. Choose a newer version, or turn off the patch to install only Spotify.");
                minimum = planned.Floor.ToString();
                progress.Report(new("Preparing", $"Staging BlockTheSpot ({planned.Label})", 10));
                await Task.Run(() => PatchFiles.Stage(staging, planned));
                staged = planned;
            }
            if (await platform.IsStoreInstalledAsync(token) && !request.RemoveStoreEdition)
                throw new InvalidOperationException("Microsoft Store Spotify is installed. Enable 'Replace Microsoft Store edition' to switch to the desktop app.");

            var setup = Path.Combine(staging, "SpotifySetup.exe");
            if (needsSetup)
            {
                await DownloadSpotifyAsync(request.Choice, setup, progress, token);
                progress.Report(new("Verifying", "Checking Spotify's digital signature", 62));
                await platform.VerifySpotifyPublisherAsync(setup, token);
            }
            token.ThrowIfCancellationRequested();
            // Cancellation is intentionally disabled once setup or file replacement starts.
            // Let these short critical operations finish instead of leaving a partial install.
            progress.Report(new("Installing", "Downloads verified. Finishing safely…", 65, false));
            await platform.StopSpotifyAsync(CancellationToken.None);
            if (request.RemoveStoreEdition && await platform.IsStoreInstalledAsync(CancellationToken.None))
                await platform.RemoveStoreAsync(CancellationToken.None);
            if (needsSetup)
                await platform.RunSetupAsync(setup, minimum, request.Choice, CancellationToken.None);
            var actual = platform.Inspect().Version ?? throw new InvalidOperationException("Spotify setup did not produce a desktop installation.");
            SpotifyVersions.ValidateInstalled(actual, minimum, needsSetup ? request.Choice : null);
            if (request.ApplyPatch)
            {
                Compatibility.ValidateInstalled(actual, request.AllowUntested);
                // Setup may install a slightly different build than planned; re-stage if its kit differs.
                var kit = Compatibility.KitFor(actual)
                    ?? throw new InvalidOperationException($"Spotify {actual} is older than the earliest kit BlockTheSpot bundles.");
                if (staged?.Id != kit.Id)
                {
                    progress.Report(new("Preparing", $"Spotify {actual} needs the {kit.Label}", 80, false));
                    await Task.Run(() => PatchFiles.Stage(staging, kit));
                }
            }
            platform.ValidateInstalledArchitecture();
            await platform.StopSpotifyAsync(CancellationToken.None);
            if (request.ApplyPatch)
            {
                progress.Report(new("Applying patch", "Saving original files and applying BlockTheSpot", 85, false));
                // Disk work stays off the UI thread; failed replacements restore the snapshot.
                await Task.Run(() => patch.Apply(platform.SpotifyDirectory, staging, needsSetup));
                // config.ini no longer blocks /desktop-update/, so the About panel keeps its version
                // and update status; the updater itself is stopped here instead.
                platform.SetUpdatesBlocked(true);
                if (request.AddPanel && XpuiInjection.IsAvailable(platform.SpotifyDirectory))
                {
                    progress.Report(new("Adding panel", "Adding the BlockTheSpot panel to Spotify's settings", 92, false));
                    // A failed injection leaves Spotify's own bundle in place; the patch itself is already done.
                    try
                    {
                        await Task.Run(() => XpuiInjection.Apply(platform.SpotifyDirectory, new Dictionary<string, object?>
                        {
                            ["appVersion"] = AppVersion,
                            ["kit"] = Compatibility.KitFor(actual)?.Id,
                            ["spotifyVersion"] = actual,
                            ["updatesBlocked"] = true,
                        }));
                    }
                    catch (Exception error) when (error is IOException or InvalidDataException or UnauthorizedAccessException)
                    {
                        progress.Report(new("Adding panel", $"Spotify's settings panel was not added: {error.Message}", 92, false));
                    }
                }
                else if (!request.AddPanel) await Task.Run(() => XpuiInjection.Restore(platform.SpotifyDirectory));
            }
            else
            {
                // Setup wrote a fresh chrome_elf.dll, so leftover patch files and the old backup
                // would only misreport the installation as patched or restore the wrong DLL later.
                progress.Report(new("Cleaning up", "Removing previous BlockTheSpot files", 85, false));
                await Task.Run(() => patch.Discard(platform.SpotifyDirectory));
                await Task.Run(() => XpuiInjection.Restore(platform.SpotifyDirectory));
                platform.SetUpdatesBlocked(false);
            }
            if (request.LaunchSpotify) platform.LaunchSpotify();
            var done = request.ApplyPatch
                ? $"Spotify {actual} is ready ({Compatibility.KitFor(actual)?.Label ?? "no kit"})."
                : $"Spotify {actual} installed without the patch.";
            progress.Report(new("Completed", done, 100, false));
        }
        finally
        {
            try { if (Directory.Exists(staging)) Directory.Delete(staging, true); }
            catch (IOException) { }
            catch (UnauthorizedAccessException) { }
            gate.Release();
        }
    }

    // Spotify's own link is tried first (its permanent URL with If-Match, or a versioned link that
    // Spotify has usually expired with HTTP 403), then the CI archive copy, then the LoadSpot mirror.
    // A failed request moves on; a SHA-256, size or executable mismatch does not, because a wrong
    // file is suspicious rather than missing.
    private async Task DownloadSpotifyAsync(SpotifyChoice choice, string target, IProgress<InstallProgress> progress, CancellationToken token)
    {
        var urls = choice.Urls.ToList();
        for (var index = 0; index < urls.Count; index++)
        {
            var url = urls[index];
            var origin = SpotifyChoice.SourceOf(url);
            var transfer = new InlineProgress<TransferProgress>(p =>
                progress.Report(new("Downloading Spotify", $"{p.Label} · {origin}", 20 + p.Percent * .4)));
            progress.Report(new("Downloading Spotify", $"Connecting to {url.Host}", 20));
            try
            {
                await downloads.FileAsync(url, target, choice.Size, false, transfer, token, choice.Sha256, url == Sources.LatestSpotify ? choice.ETag : null);
                return;
            }
            catch (HttpRequestException error) when (index < urls.Count - 1)
            {
                var reason = error.StatusCode is { } code ? $"HTTP {(int)code}" : "connection failed";
                progress.Report(new("Switching source", $"{origin} did not serve this version ({reason}). Trying {SpotifyChoice.SourceOf(urls[index + 1])}.", 20));
            }
        }
    }

    public async Task RestoreAsync(IProgress<InstallProgress> progress, CancellationToken token)
    {
        if (!await gate.WaitAsync(0, token)) throw new InvalidOperationException("An installation is already running.");
        try
        {
            token.ThrowIfCancellationRequested();
            progress.Report(new("Restoring", "Restoring Spotify's original files", 30, false));
            await platform.StopSpotifyAsync(CancellationToken.None);
            await Task.Run(() => patch.Restore(platform.SpotifyDirectory));
            await Task.Run(() => XpuiInjection.Restore(platform.SpotifyDirectory));
            platform.SetUpdatesBlocked(false);
            progress.Report(new("Completed", "Original Spotify files restored.", 100, false));
        }
        finally { gate.Release(); }
    }
}

public sealed class InlineProgress<T>(Action<T> report) : IProgress<T>
{
    public void Report(T value) => report(value);
}
