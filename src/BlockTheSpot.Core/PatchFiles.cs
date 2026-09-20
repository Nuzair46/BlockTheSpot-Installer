namespace BlockTheSpot.Core;

/// <summary>
/// Writes a bundled BlockTheSpot kit into the staging directory. chrome_elf.dll is the same proxy
/// for every kit; blockthespot.dll and config.ini come from the kit's own embedded resources
/// (see <see cref="PatchKit"/>).
/// </summary>
public static class PatchFiles
{
    public static void Stage(string directory, PatchKit kit)
    {
        Directory.CreateDirectory(directory);
        Write(directory, "chrome_elf.dll", "BlockTheSpot.Patch.chrome_elf.dll");
        Write(directory, "blockthespot.dll", kit.ResourcePrefix + "blockthespot.dll");
        Write(directory, "config.ini", kit.ResourcePrefix + "config.ini");
    }

    private static void Write(string directory, string name, string resource)
    {
        using var stream = typeof(PatchFiles).Assembly.GetManifestResourceStream(resource)
            ?? throw new InvalidOperationException($"The bundled patch file {name} ({resource}) is missing from this installer.");
        using var file = File.Create(Path.Combine(directory, name));
        stream.CopyTo(file);
    }
}
