namespace BlockTheSpot.Core;

public interface IFileOperations
{
    void Copy(string source, string destination);
    void Delete(string path);
}

public sealed class FileOperations : IFileOperations
{
    public void Copy(string source, string destination) => File.Copy(source, destination, true);
    public void Delete(string path) => File.Delete(path);
}

public sealed class PatchTransaction(IFileOperations? operations = null)
{
    private readonly IFileOperations files = operations ?? new FileOperations();
    private static readonly string[] Names = ["chrome_elf.dll", "chrome_elf_required.dll", "blockthespot.dll", "config.ini"];

    public void Apply(string spotifyDirectory, string stagingDirectory, bool spotifyWasReinstalled)
    {
        foreach (var name in new[] { "chrome_elf.dll", "blockthespot.dll", "config.ini" })
            if (!File.Exists(Path.Combine(stagingDirectory, name)))
                throw new FileNotFoundException($"The staged patch is missing {name}. No files were changed.");
        var chrome = Path.Combine(spotifyDirectory, "chrome_elf.dll");
        var backup = Path.Combine(spotifyDirectory, "chrome_elf_required.dll");
        if (!File.Exists(chrome) && !File.Exists(backup))
            throw new FileNotFoundException("Spotify's original chrome_elf.dll is missing. Reinstall Spotify before applying the patch.");
        Run(spotifyDirectory, () =>
        {
            if (!File.Exists(backup) || spotifyWasReinstalled)
            {
                if (!File.Exists(chrome)) throw new FileNotFoundException("Spotify's original DLL is missing.");
                files.Copy(chrome, backup);
            }
            foreach (var name in new[] { "chrome_elf.dll", "blockthespot.dll", "config.ini" })
                files.Copy(Path.Combine(stagingDirectory, name), Path.Combine(spotifyDirectory, name));
        });
    }

    public void Restore(string spotifyDirectory)
    {
        var backup = Path.Combine(spotifyDirectory, "chrome_elf_required.dll");
        if (!File.Exists(backup))
        {
            if (File.Exists(Path.Combine(spotifyDirectory, "blockthespot.dll")))
                throw new InvalidOperationException("The original Spotify DLL backup is missing. Reinstall Spotify to restore its original files.");
            return;
        }
        Run(spotifyDirectory, () =>
        {
            files.Copy(backup, Path.Combine(spotifyDirectory, "chrome_elf.dll"));
            files.Delete(Path.Combine(spotifyDirectory, "blockthespot.dll"));
            files.Delete(Path.Combine(spotifyDirectory, "config.ini"));
            files.Delete(backup);
        });
    }

    /// <summary>Removes patch files and the DLL backup after Spotify setup replaced the patched DLL with a fresh original.</summary>
    public void Discard(string spotifyDirectory)
    {
        var leftovers = new[] { "blockthespot.dll", "config.ini", "chrome_elf_required.dll" }
            .Select(name => Path.Combine(spotifyDirectory, name)).Where(File.Exists).ToList();
        if (leftovers.Count == 0) return;
        Run(spotifyDirectory, () => { foreach (var path in leftovers) files.Delete(path); });
    }

    private void Run(string directory, Action change)
    {
        Directory.CreateDirectory(directory);
        var snapshot = Path.Combine(directory, ".blockthespot-recovery-" + Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(snapshot);
        var previous = new HashSet<string>();
        var keepRecovery = false;
        try
        {
            foreach (var name in Names)
            {
                var target = Path.Combine(directory, name);
                if (Directory.Exists(target)) throw new IOException($"{name} is a directory; cannot safely update Spotify.");
                if (!File.Exists(target)) continue;
                File.Copy(target, Path.Combine(snapshot, name));
                previous.Add(name);
            }
            try { change(); }
            catch (Exception error)
            {
                try
                {
                    foreach (var name in Names)
                    {
                        var target = Path.Combine(directory, name);
                        if (previous.Contains(name)) File.Copy(Path.Combine(snapshot, name), target, true);
                        else File.Delete(target);
                    }
                }
                catch (Exception recoveryError)
                {
                    keepRecovery = true;
                    throw new AggregateException($"Could not restore every file. Recovery copies are saved at {snapshot}.", error, recoveryError);
                }
                throw new IOException("The patch could not be completed. The previous files were restored.", error);
            }
        }
        finally
        {
            if (!keepRecovery)
            {
                try { Directory.Delete(snapshot, true); }
                catch (IOException) { /* A harmless recovery copy can remain if another process holds it. */ }
                catch (UnauthorizedAccessException) { }
            }
        }
    }
}
