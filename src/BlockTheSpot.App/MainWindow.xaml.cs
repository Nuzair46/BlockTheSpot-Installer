using System.ComponentModel;
using System.Windows;
using System.Windows.Navigation;

namespace BlockTheSpot.App;

public partial class MainWindow : Window
{
    public MainViewModel Model { get; }
    public MainWindow(bool preview = false)
    {
        InitializeComponent();
        Model = new(preview);
        DataContext = Model;
        if (!preview) Loaded += async (_, _) =>
        {
            try { await Model.InitializeAsync(); }
            catch (OperationCanceledException) { }
            catch (Exception error) { MessageBox.Show(this, error.Message, "Startup", MessageBoxButton.OK, MessageBoxImage.Error); }
        };
        Closing += OnClosing;
        Closed += (_, _) => Model.Dispose();
    }

    private void OnClosing(object? sender, CancelEventArgs e)
    {
        if (!Model.IsBusy) return;
        e.Cancel = true;
        if (Model.CanCancel) Model.Cancel();
        else MessageBox.Show(this, "Spotify setup is finishing. This window can be closed when it completes.", "Installation in progress");
    }

    private void OpenLink(object sender, RequestNavigateEventArgs e)
    { MainViewModel.OpenUrl(e.Uri.AbsoluteUri); e.Handled = true; }
}
