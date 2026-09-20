using System.Collections.ObjectModel;
using System.ComponentModel;
using System.Diagnostics;
using System.IO;
using System.Net.Http;
using System.Reflection;
using System.Runtime.CompilerServices;
using System.Text.Json;
using System.Windows;
using System.Windows.Input;
using BlockTheSpot.Core;
using Microsoft.Win32;

namespace BlockTheSpot.App;

public sealed class MainViewModel : INotifyPropertyChanged, IDisposable
{
    private const string SourceNote = "Spotify's own link is tried first, then the LoadSpot mirror. Every installer's Spotify signature is checked before setup runs.";
    private readonly HttpClient http = Downloads.CreateClient();
    private readonly WindowsSpotifyPlatform platform = new();
    private readonly CatalogService catalog;
    private readonly InstallerService installer;
    private readonly CancellationTokenSource lifetime = new();
    private CancellationTokenSource? operation;
    private readonly Queue<string> log = new();
    private bool busy, loading, canCancel, reinstall = true, launch = true, removeStore, showLog, showAll, applyPatch = true, addPanel = true;
    private IReadOnlyList<SpotifyChoice> catalogChoices = [];
    private string status = "Getting ready", detail = "Loading Spotify versions…", installedLabel = "Checking your installation…";
    private string hint = "Loading the Spotify version catalog", versionLabel = "v" + AppVersion, lastStage = "", filter = "";
    private SpotifyChoice? selected;
    private double progressValue;

    public static string AppVersion => Assembly.GetExecutingAssembly().GetName().Version?.ToString(3) ?? "dev";
    public ObservableCollection<SpotifyChoice> Choices { get; } = [];
    public event PropertyChangedEventHandler? PropertyChanged;
    public RelayCommand RefreshCommand { get; }
    public RelayCommand InstallCommand { get; }
    public RelayCommand RestoreCommand { get; }
    public RelayCommand CancelCommand { get; }
    public RelayCommand ReleasesCommand { get; }
    public RelayCommand SaveLogCommand { get; }
    public bool CanEdit => !busy && !loading;
    public bool IsBusy => busy;
    public bool IsLoading => loading;
    public bool CanCancel => busy && canCancel;
    public bool ReinstallSpotify { get => reinstall; set { Set(ref reinstall, value); Notify(nameof(PrimaryAction)); Notify(nameof(PatchMethodLabel)); } }
    public bool LaunchSpotify { get => launch; set => Set(ref launch, value); }
    public bool RemoveStoreEdition { get => removeStore; set => Set(ref removeStore, value); }
    public bool ShowLog { get => showLog; set => Set(ref showLog, value); }
    /// <summary>Lists every catalog build and accepts typed versions. Off, the current and legacy pins are offered.</summary>
    public bool ShowAllVersions { get => showAll; set { Set(ref showAll, value); PopulateChoices(); } }
    public bool ApplyPatch { get => applyPatch; set { Set(ref applyPatch, value); Notify(nameof(PrimaryAction)); Notify(nameof(PatchMethodLabel)); } }
    /// <summary>Adds the BlockTheSpot section to Spotify's own settings, and an active line next to its version.</summary>
    public bool AddPanel { get => addPanel; set => Set(ref addPanel, value); }
    public string Filter { get => filter; set { Set(ref filter, value); PopulateChoices(); } }
    public SpotifyChoice? SelectedChoice { get => selected; set { Set(ref selected, value); Notify(nameof(PatchMethodLabel)); } }
    public string Status { get => status; private set => Set(ref status, value); }
    public string StatusDetail { get => detail; private set => Set(ref detail, value); }
    public string InstalledLabel { get => installedLabel; private set => Set(ref installedLabel, value); }
    public string CatalogHint { get => hint; private set => Set(ref hint, value); }
    public string VersionLabel { get => versionLabel; private set => Set(ref versionLabel, value); }
    public string AllVersionsLabel => catalogChoices.Count > 1 ? $"All versions ({catalogChoices.Count - 1})" : "All versions";
    public string PrimaryAction => ApplyPatch ? "Install BlockTheSpot" : ReinstallSpotify ? "Install Spotify only" : "Nothing to install";
    // Names the bundled kit that will actually be applied, given the version that will be running.
    public string PatchMethodLabel
    {
        get
        {
            if (!ApplyPatch || SelectedChoice is null) return "The patch is off. Only Spotify will be installed.";
            var version = ReinstallSpotify ? SelectedChoice.FullVersion : platform.Inspect().Version ?? SelectedChoice.FullVersion;
            if (version is null)
                return $"Patch: {Compatibility.CurrentKit.Label} after setup.";
            var kit = Compatibility.KitFor(version);
            return kit is null
                ? $"Spotify {version} is below the earliest kit ({Compatibility.LegacyKit.Floor})."
                : $"Patch: {kit.Label}{(kit == Compatibility.CurrentKit ? " (current)" : "")} for Spotify {version}.";
        }
    }
    public double ProgressValue { get => progressValue; private set { Set(ref progressValue, value); Notify(nameof(ProgressLabel)); } }
    public string ProgressLabel => busy ? $"{ProgressValue:0}%" : "";
    public string LogText => string.Join(Environment.NewLine, log);

    public MainViewModel(bool preview)
    {
        var downloads = new Downloads(http);
        catalog = new(downloads);
        installer = new(downloads, platform);
        RefreshCommand = new(() => ObserveAsync(RefreshAsync), () => CanEdit);
        InstallCommand = new(() => ObserveAsync(() => RunAsync(false)), () => CanEdit && SelectedChoice is not null && (ApplyPatch || ReinstallSpotify));
        RestoreCommand = new(() => ObserveAsync(() => RunAsync(true)), () => CanEdit);
        CancelCommand = new(Cancel, () => CanCancel);
        ReleasesCommand = new(() => OpenUrl(Sources.Repository + "/releases/latest"));
        SaveLogCommand = new(SaveLog);
        if (preview)
        {
            catalogChoices = [Compatibility.TestedChoice, Compatibility.LegacyChoice, SpotifyChoice.Latest];
            PopulateChoices();
            InstalledLabel = "Spotify 1.3.1.234 · desktop edition · not patched";
            CatalogHint = SourceNote;
            Status = "Ready when you are";
            StatusDetail = "Spotify 1.3.1.234 will be installed before patching.";
        }
    }

    public async Task InitializeAsync()
    {
        Inspect();
        await Task.WhenAll(RefreshAsync(), CheckUpdateAsync());
    }

    private void Inspect()
    {
        var current = platform.Inspect();
        InstalledLabel = current.Version is null ? "Spotify is not installed · the selected version will be installed" :
            $"Spotify {current.Version} · desktop edition · {(current.Patched ? "BlockTheSpot installed" : "not patched")}";
    }

    private async Task RefreshAsync()
    {
        loading = true; ChangedState();
        try
        {
            var result = await catalog.LoadAsync(lifetime.Token);
            catalogChoices = result.Choices;
            PopulateChoices();
            CatalogHint = result.Warning ?? SourceNote;
            Status = result.Warning is null ? "Ready when you are" : "Version list needs attention";
            StatusDetail = result.Warning ?? "Pick a version and options, then install.";
            AddLog(result.Warning ?? $"Loaded {result.Choices.Count - 1} Spotify versions from the catalog.");
        }
        finally { loading = false; ChangedState(); }
    }

    private async Task RunAsync(bool restore)
    {
        if (WindowsSpotifyPlatform.IsAdministrator)
            throw new InvalidOperationException("Open BlockTheSpot normally, without 'Run as administrator'. Spotify installs for your Windows account.");
        busy = true; canCancel = true; lastStage = ""; ProgressValue = 0;
        ChangedState();
        using var cancellation = CancellationTokenSource.CreateLinkedTokenSource(lifetime.Token);
        operation = cancellation;
        var updates = new Progress<InstallProgress>(update =>
        {
            Status = update.Stage; StatusDetail = update.Detail; ProgressValue = update.Percent;
            canCancel = update.CanCancel; ChangedState();
            if (lastStage != update.Stage) { AddLog($"{update.Stage}: {update.Detail}"); lastStage = update.Stage; }
        });
        try
        {
            if (restore) await installer.RestoreAsync(updates, cancellation.Token);
            else
            {
                var choice = SelectedChoice!;
                AddLog($"Selected {choice.Title} ({choice.Badge}) from {string.Join(" → ", choice.Urls.Select(u => u.Host))}.");
                await installer.InstallAsync(new(choice, ReinstallSpotify, LaunchSpotify, RemoveStoreEdition, ShowAllVersions, ApplyPatch, AddPanel), updates, cancellation.Token);
            }
            Inspect();
        }
        catch (OperationCanceledException) when (cancellation.IsCancellationRequested)
        {
            Status = "Cancelled"; StatusDetail = "Preparation stopped. No patch was applied."; AddLog(StatusDetail);
        }
        finally { operation = null; busy = false; canCancel = false; ChangedState(); }
    }

    public void Cancel()
    {
        if (!CanCancel) return;
        canCancel = false; operation?.Cancel();
        StatusDetail = "Cancelling the download…"; ChangedState();
    }

    private void PopulateChoices()
    {
        var previous = SelectedChoice;
        var tested = catalogChoices.FirstOrDefault(c => string.Equals(c.FullVersion, Compatibility.TestedVersion, StringComparison.OrdinalIgnoreCase)) ?? Compatibility.TestedChoice;
        var list = new List<SpotifyChoice> { tested with { Recommended = true } };
        SpotifyChoice? typed = null;
        // The legacy pin is a second fixed entry; in the full list it already appears from the catalog.
        if (!ShowAllVersions && !list.Any(c => c.IsLegacyPin))
            list.Add(Compatibility.LegacyChoice with { Recommended = false });
        if (ShowAllVersions)
        {
            var search = Filter.Trim();
            list.AddRange(catalogChoices.Where(c => c.FullVersion != tested.FullVersion && (search.Length == 0 || Matches(c, search))).Select(c => c with { Recommended = false }));
            // A full version or link that is not in the catalog is still installable; the download is verified like any other.
            typed = SpotifyVersions.TryCustom(search);
            if (typed is not null && !list.Any(c => Offers(c, typed.Url))) list.Add(typed);
        }
        Choices.Clear();
        foreach (var choice in list) Choices.Add(choice);
        // A deliberately typed version wins over the previous selection.
        SelectedChoice = (typed is null ? null : Choices.FirstOrDefault(c => Offers(c, typed.Url)))
            ?? Choices.FirstOrDefault(c => c.Url == previous?.Url) ?? Choices[0];
        Notify(nameof(AllVersionsLabel));
        Notify(nameof(PatchMethodLabel));
    }

    private static bool Matches(SpotifyChoice choice, string search) =>
        choice.Title.Contains(search, StringComparison.OrdinalIgnoreCase) || choice.Urls.Any(u => u.AbsoluteUri.Contains(search, StringComparison.OrdinalIgnoreCase));
    private static bool Offers(SpotifyChoice choice, Uri url) => choice.Url == url || choice.Mirror == url;


    private async Task CheckUpdateAsync()
    {
        try
        {
            var json = await new Downloads(http).TextAsync(Sources.LatestRelease, lifetime.Token);
            using var release = JsonDocument.Parse(json);
            var tag = release.RootElement.GetProperty("tag_name").GetString();
            if (Version.TryParse(tag?.TrimStart('v'), out var latest) && Version.TryParse(AppVersion, out var current) && latest > current)
                VersionLabel = $"v{AppVersion} · Update available";
        }
        catch (Exception error) when (error is HttpRequestException or JsonException or IOException or OperationCanceledException)
        { AddLog("Release check unavailable. Releases can still be opened from the version button."); }
    }

    private async void ObserveAsync(Func<Task> action)
    {
        try { await action(); }
        catch (OperationCanceledException) when (lifetime.IsCancellationRequested) { }
        catch (Exception error)
        {
            Status = "Couldn't finish"; StatusDetail = error.Message;
            AddLog(error.ToString()); ShowLog = true;
        }
    }

    private void SaveLog()
    {
        var dialog = new SaveFileDialog { FileName = $"BlockTheSpot-{DateTime.Now:yyyyMMdd-HHmm}.log", Filter = "Log file (*.log)|*.log" };
        if (dialog.ShowDialog() != true) return;
        try { File.WriteAllText(dialog.FileName, LogText); }
        catch (Exception error) when (error is IOException or UnauthorizedAccessException)
        { StatusDetail = $"Couldn't save the log: {error.Message}"; }
    }

    private void AddLog(string text)
    {
        log.Enqueue($"[{DateTime.Now:HH:mm:ss}] {text}");
        while (log.Count > 200) log.Dequeue();
        Notify(nameof(LogText));
    }

    public static void OpenUrl(string url)
    {
        try { Process.Start(new ProcessStartInfo(url) { UseShellExecute = true })?.Dispose(); }
        catch (System.ComponentModel.Win32Exception) { MessageBox.Show("Your browser could not be opened. Visit " + url, "Open link"); }
    }

    private void ChangedState()
    {
        foreach (var name in new[] { nameof(IsBusy), nameof(CanEdit), nameof(IsLoading), nameof(CanCancel), nameof(ProgressLabel) }) Notify(name);
        CommandManager.InvalidateRequerySuggested();
    }
    private void Set<T>(ref T field, T value, [CallerMemberName] string? name = null)
    { if (EqualityComparer<T>.Default.Equals(field, value)) return; field = value; Notify(name); }
    private void Notify([CallerMemberName] string? name = null) => PropertyChanged?.Invoke(this, new(name));
    public void Dispose() { lifetime.Cancel(); lifetime.Dispose(); http.Dispose(); }
}

public sealed class RelayCommand(Action action, Func<bool>? canExecute = null) : ICommand
{
    public bool CanExecute(object? parameter) => canExecute?.Invoke() ?? true;
    public void Execute(object? parameter) => action();
    public event EventHandler? CanExecuteChanged { add => CommandManager.RequerySuggested += value; remove => CommandManager.RequerySuggested -= value; }
}
