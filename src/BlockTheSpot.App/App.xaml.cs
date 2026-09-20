using System.IO;
using System.Windows;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using System.Windows.Threading;

namespace BlockTheSpot.App;

public partial class App : Application
{
    private Mutex? instance;

    protected override async void OnStartup(StartupEventArgs e)
    {
        base.OnStartup(e);
        var smokeTest = e.Args.Length == 2 && e.Args[0] == "--smoke-test";
        if (smokeTest) Directory.CreateDirectory(e.Args[1]);
        DispatcherUnhandledException += (_, args) =>
        {
            var path = smokeTest ? Path.Combine(e.Args[1], "error.txt") : Path.Combine(Path.GetTempPath(), "BlockTheSpot-startup-error.log");
            try { File.WriteAllText(path, args.Exception.ToString()); } catch (IOException) { }
            if (!smokeTest) MessageBox.Show($"BlockTheSpot encountered an error.\n\n{args.Exception.Message}\n\nLog: {path}", "BlockTheSpot");
            args.Handled = true;
            Shutdown(1);
        };
        if (!smokeTest)
        {
            instance = new Mutex(true, @"Local\BlockTheSpotInstaller", out var created);
            if (!created) { MessageBox.Show("BlockTheSpot is already open.", "BlockTheSpot"); Shutdown(); return; }
        }
        try
        {
            var window = new MainWindow(smokeTest);
            MainWindow = window;
            window.Show();
            if (!smokeTest) return;
            foreach (var key in new[] { "CardBackgroundFillColorDefaultBrush", "TextFillColorPrimaryBrush", "AccentFillColorDefaultBrush" })
                if (window.TryFindResource(key) is null) throw new InvalidOperationException("Missing Fluent resource: " + key);
            if (window.VersionPicker.Items.Count != 2 || !window.Model.InstallCommand.CanExecute(null))
                throw new InvalidOperationException("Initial UI bindings did not initialize.");
            // Drive the version picker the way a user would: list everything, filter, type a version, switch modes.
            window.Model.ShowAllVersions = true;
            if (window.VersionPicker.Items.Count != 3 || window.FilterBox.Visibility != Visibility.Visible)
                throw new InvalidOperationException("All versions did not populate the picker.");
            window.FilterBox.Text = "1.2.80.699.gd5f6ebe3";
            if (window.VersionPicker.Items.Count != 2 || window.Model.SelectedChoice?.Custom != true || window.Model.SelectedChoice.FullVersion != "1.2.80.699.gd5f6ebe3")
                throw new InvalidOperationException("A typed version did not become the selected custom choice.");
            window.FilterBox.Text = "";
            window.Model.ApplyPatch = false;
            if (window.InstallButton.Content as string != "Install Spotify only" || !window.Model.InstallCommand.CanExecute(null))
                throw new InvalidOperationException("Spotify-only mode did not update the primary action.");
            window.Model.ApplyPatch = true;
            window.Model.ShowAllVersions = false;
            if (window.VersionPicker.Items.Count != 2 || window.Model.SelectedChoice?.Recommended != true)
                throw new InvalidOperationException("Returning to the tested build did not reset the picker.");
            // RenderTargetBitmap cannot capture the compositor's Mica backdrop.
            // Use Fluent's opaque fallback only for test screenshots.
            window.SetResourceReference(Window.BackgroundProperty, "WindowBackground");
            foreach (var dark in new[] { false, true })
            {
                ThemeMode = dark ? ThemeMode.Dark : ThemeMode.Light;
                await Dispatcher.InvokeAsync(() => { }, DispatcherPriority.ApplicationIdle);
                await Task.Delay(350);
                window.UpdateLayout();
                var bitmap = new RenderTargetBitmap((int)window.ActualWidth, (int)window.ActualHeight, 96, 96, PixelFormats.Pbgra32);
                bitmap.Render(window);
                var encoder = new PngBitmapEncoder();
                encoder.Frames.Add(BitmapFrame.Create(bitmap));
                using var stream = File.Create(Path.Combine(e.Args[1], dark ? "windows-dark.png" : "windows-light.png"));
                encoder.Save(stream);
            }
            File.WriteAllText(Path.Combine(e.Args[1], "result.txt"), "Fluent resources, initial bindings, version picker interactions, light theme and dark theme rendered.");
            Shutdown(0);
        }
        catch (Exception error)
        {
            if (!smokeTest) throw;
            File.WriteAllText(Path.Combine(e.Args[1], "error.txt"), error.ToString());
            Shutdown(1);
        }
    }

    protected override void OnExit(ExitEventArgs e) { instance?.Dispose(); base.OnExit(e); }
}
