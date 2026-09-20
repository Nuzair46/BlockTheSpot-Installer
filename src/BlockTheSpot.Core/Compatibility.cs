namespace BlockTheSpot.Core;

/// <summary>
/// One bundled BlockTheSpot kit and the lowest Spotify version it patches. Kits are ordered by
/// <see cref="Floor"/>; a kit covers [Floor, next kit's Floor), and the highest-floor kit is the
/// current one and keeps taking every newer Spotify build. <see cref="Id"/> is the folder under
/// <c>Patch/</c> and the resource prefix. To add a kit: drop <c>Patch/&lt;id&gt;/{blockthespot.dll,config.ini}</c>,
/// embed them in the csproj, and add one row to <see cref="Compatibility.Kits"/>.
/// </summary>
public sealed record PatchKit(string Id, Version Floor, string Tag, string Label)
{
    public string ResourcePrefix => $"BlockTheSpot.Patch.{Id}.";
}

public static class Compatibility
{
    // Ordered by Floor ascending; the last entry is the current kit. Neither the floors nor the pins
    // follow the newest catalog entry, and an upstream config or catalog update must never move them.
    //   legacy  — upstream Nuzair46 kit shipped for 1.2.93.667 (kernel32 IAT), Spotify 1.2.70–1.2.95.
    //   current — kit adapted for Spotify ≥ 1.2.96: two IAT jumps NOP'd in blockthespot.dll and
    //             config.ini's xpui signatures rebuilt for 1.3.1.234.
    public static IReadOnlyList<PatchKit> Kits { get; } =
    [
        new("legacy", new(1, 2, 70, 0), "Legacy", "legacy kit · kernel32 IAT"),
        new("current", new(1, 2, 96, 0), "Current", "current kit · api-ms IAT"),
    ];

    /// <summary>The newest kit; it patches everything at or above its floor.</summary>
    public static PatchKit CurrentKit => Kits[^1];
    public static PatchKit LegacyKit => Kits[0];

    // The default pin (a current-kit build) and the fallback pin (a legacy-kit build). 1.3.1.234 is
    // the default so a fresh install produces a modern Spotify plus the current kit; 1.2.93.667 is the
    // legacy build, the one verified with the original kit on 2026-09-18.
    public const string TestedVersion = "1.3.1.234.g59d6bf59";
    public const string LegacyVersion = "1.2.93.667.g7b5cc0ce";

    // SHA-256 of each build's file as downloaded on 2026-09-18 (ProductVersion checked, Authenticode
    // O=Spotify AB). Pins the exact bytes on top of the signature check.
    public static SpotifyChoice TestedChoice { get; } = new(TestedVersion,
        new("https://loadspot.amd64fox1.workers.dev/download/spotify_installer-1.3.1.234.g59d6bf59-x64.exe"),
        "16.09.2026", 148153000, true, Sha256: "6c25d92dd38ddbfc1e4970765e865766d8ae2ef1758c6af8e9122c5050ee66dc");

    public static SpotifyChoice LegacyChoice { get; } = new(LegacyVersion,
        new("https://loadspot.amd64fox1.workers.dev/download/spotify_installer-1.2.93.667.g7b5cc0ce-x64.exe"),
        "01.07.2026", 146096232, false, Sha256: "614741858a7def3fb21da385312fe7b114dd2873f6d79fb3d980a979af560ba0");

    /// <summary>The kit whose range contains <paramref name="version"/>: the highest floor at or below it, or null when the version is below every kit.</summary>
    public static PatchKit? KitFor(string version)
    {
        var value = SpotifyVersions.Parse(version);
        PatchKit? match = null;
        foreach (var kit in Kits) if (value >= kit.Floor) match = kit;
        return match;
    }

    /// <summary>The chip shown for a build: the current kit always reads "Current", earlier kits keep their tag.</summary>
    public static string MethodOf(string? version)
    {
        if (version is null) return CurrentKit.Tag;
        var kit = KitFor(version);
        return kit is null ? "Unsupported" : kit == CurrentKit ? "Current" : kit.Tag;
    }

    public static void ValidateChoice(SpotifyChoice choice, bool allowUntested)
    {
        if (!allowUntested && !string.Equals(choice.FullVersion, TestedVersion, StringComparison.OrdinalIgnoreCase)
            && !string.Equals(choice.FullVersion, LegacyVersion, StringComparison.OrdinalIgnoreCase))
            throw new InvalidOperationException($"The pinned builds are {TestedVersion} (current kit) and {LegacyVersion} (legacy kit). Tick 'All versions' to choose a different build.");
    }

    public static void ValidateInstalled(string installed, bool allowUntested)
    {
        if (allowUntested) return;
        var actual = SpotifyVersions.Parse(installed);
        if (actual != SpotifyVersions.Parse(TestedVersion) && actual != SpotifyVersions.Parse(LegacyVersion))
            throw new InvalidOperationException($"Spotify {installed} has not been pinned in this release. Enable 'Install this Spotify version' to install {TestedVersion}, or tick 'All versions' to keep it.");
    }
}
