using System.IO.Compression;
using System.Text;
using System.Text.Json;

namespace BlockTheSpot.Core;

/// <summary>
/// Adds the BlockTheSpot panel to Spotify's web bundle (Apps/xpui.spa, a zip): one extra script
/// file and one tag in index.html. The untouched bundle is kept next to it, so restoring is a copy
/// and re-applying always starts from Spotify's own file rather than stacking on a previous run.
/// The rewritten bundle is validated before it replaces the original: if anything about it looks
/// wrong the original stays in place, because a corrupt xpui.spa stops Spotify from starting.
/// </summary>
public static class XpuiInjection
{
    public const string ScriptName = "blockthespot-ui.js";
    private const string IndexName = "index.html";
    private const string Closing = "</body>";
    private static readonly string Tag = $"<script defer=\"defer\" src=\"/{ScriptName}\"></script>";

    public static string SpaPath(string spotifyDirectory) => Path.Combine(spotifyDirectory, "Apps", "xpui.spa");
    public static string BackupPath(string spotifyDirectory) => SpaPath(spotifyDirectory) + ".bts-backup";

    /// <summary>True when this Spotify has the web bundle at all (the Store edition and partial installs may not).</summary>
    public static bool IsAvailable(string spotifyDirectory) => File.Exists(SpaPath(spotifyDirectory));

    public static bool IsInjected(string spotifyDirectory)
    {
        if (!IsAvailable(spotifyDirectory)) return false;
        try
        {
            using var archive = ZipFile.OpenRead(SpaPath(spotifyDirectory));
            return archive.GetEntry(ScriptName) is not null;
        }
        catch (InvalidDataException) { return false; }
    }

    /// <param name="info">Values the panel shows: app version, kit, Spotify version, updater state.</param>
    public static void Apply(string spotifyDirectory, IReadOnlyDictionary<string, object?> info)
    {
        var spa = SpaPath(spotifyDirectory);
        if (!File.Exists(spa)) throw new FileNotFoundException($"Spotify's web bundle is missing: {spa}");
        var backup = BackupPath(spotifyDirectory);
        // The backup is the pristine bundle; a second run rebuilds from it instead of from a patched file.
        if (!File.Exists(backup)) File.Copy(spa, backup);

        var script = new StringBuilder()
            .Append("window.__BTS_INFO__ = ").Append(JsonSerializer.Serialize(info)).AppendLine(";")
            .AppendLine(ReadScript())
            .ToString();

        var temporary = spa + ".bts-new";
        try
        {
            int expected;
            using (var source = ZipFile.OpenRead(backup))
            {
                if (source.GetEntry(IndexName) is null)
                    throw new InvalidDataException($"{IndexName} is missing from Spotify's web bundle; not modifying it.");
                expected = source.Entries.Count(entry => entry.FullName != ScriptName) + 1;
                using var output = new FileStream(temporary, FileMode.Create, FileAccess.Write, FileShare.None);
                using var archive = new ZipArchive(output, ZipArchiveMode.Create);
                foreach (var entry in source.Entries)
                {
                    if (entry.FullName == ScriptName) continue;
                    var copy = archive.CreateEntry(entry.FullName, CompressionLevel.Optimal);
                    using var reading = entry.Open();
                    using var writing = copy.Open();
                    if (entry.FullName == IndexName) WriteIndex(reading, writing);
                    else reading.CopyTo(writing);
                }
                using var added = archive.CreateEntry(ScriptName, CompressionLevel.Optimal).Open();
                added.Write(Encoding.UTF8.GetBytes(script));
            }
            Validate(temporary, expected);
            File.Move(temporary, spa, true);
        }
        catch
        {
            if (File.Exists(temporary)) File.Delete(temporary);
            throw;
        }
    }

    /// <summary>Puts Spotify's own bundle back. Safe to call when nothing was injected.</summary>
    public static void Restore(string spotifyDirectory)
    {
        var backup = BackupPath(spotifyDirectory);
        if (!File.Exists(backup)) return;
        File.Copy(backup, SpaPath(spotifyDirectory), true);
        File.Delete(backup);
    }

    private static void WriteIndex(Stream reading, Stream writing)
    {
        using var reader = new StreamReader(reading, Encoding.UTF8);
        var html = reader.ReadToEnd();
        if (!html.Contains(Tag, StringComparison.Ordinal))
        {
            var at = html.LastIndexOf(Closing, StringComparison.OrdinalIgnoreCase);
            html = at < 0 ? html + Tag : html.Insert(at, Tag);
        }
        var bytes = Encoding.UTF8.GetBytes(html);
        writing.Write(bytes, 0, bytes.Length);
    }

    // A bundle that cannot be opened, lost entries, or lost the tag would leave Spotify unable to start.
    private static void Validate(string path, int expected)
    {
        using var archive = ZipFile.OpenRead(path);
        if (archive.Entries.Count != expected)
            throw new InvalidDataException($"The rebuilt web bundle has {archive.Entries.Count} entries, expected {expected}.");
        var script = archive.GetEntry(ScriptName) ?? throw new InvalidDataException("The rebuilt web bundle is missing the panel script.");
        if (script.Length == 0) throw new InvalidDataException("The panel script was written empty.");
        var index = archive.GetEntry(IndexName) ?? throw new InvalidDataException($"The rebuilt web bundle is missing {IndexName}.");
        using var reader = new StreamReader(index.Open(), Encoding.UTF8);
        if (!reader.ReadToEnd().Contains(Tag, StringComparison.Ordinal))
            throw new InvalidDataException($"{IndexName} does not reference the panel script.");
    }

    private static string ReadScript()
    {
        using var stream = typeof(XpuiInjection).Assembly.GetManifestResourceStream($"BlockTheSpot.Patch.{ScriptName}")
            ?? throw new InvalidOperationException("The bundled panel script is missing from this installer.");
        using var reader = new StreamReader(stream, Encoding.UTF8);
        return reader.ReadToEnd();
    }
}
