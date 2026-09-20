using System.IO;
using BlockTheSpot.Core;
using Xunit;

namespace BlockTheSpot.Tests;

public sealed class CompatibilityTests
{
    [Fact]
    public void TestedDefaultIsPinnedAndCannotFollowLatestAutomatically()
    {
        Assert.Equal("1.3.1.234.g59d6bf59", Compatibility.TestedVersion);
        Assert.Equal("1.2.93.667.g7b5cc0ce", Compatibility.LegacyVersion);
        Compatibility.ValidateChoice(Compatibility.TestedChoice, false);
        Compatibility.ValidateChoice(Compatibility.LegacyChoice, false);
        Assert.Throws<InvalidOperationException>(() => Compatibility.ValidateChoice(SpotifyChoice.Latest, false));
        Assert.Throws<InvalidOperationException>(() => Compatibility.ValidateInstalled("1.3.1.223", false));
        Compatibility.ValidateChoice(SpotifyChoice.Latest, true);
    }

    [Fact]
    public async Task TestedChoicePinsItsFileHashSoADifferentPayloadIsRefused()
    {
        Assert.Equal("6c25d92dd38ddbfc1e4970765e865766d8ae2ef1758c6af8e9122c5050ee66dc", Compatibility.TestedChoice.Sha256);
        using var client = new HttpClient(new FakeHandler(_ => new(System.Net.HttpStatusCode.OK) { Content = new ByteArrayContent(new byte[512]) }));
        var target = Path.Combine(Path.GetTempPath(), "BlockTheSpot.Tests-" + Guid.NewGuid().ToString("N") + ".exe");
        await Assert.ThrowsAsync<InvalidDataException>(() => new Downloads(client).FileAsync(Compatibility.TestedChoice.Url, target, 0, false, null, CancellationToken.None, Compatibility.TestedChoice.Sha256));
        Assert.False(File.Exists(target));
    }

    [Fact]
    public void VersionHashComparisonIgnoresCase()
    {
        Compatibility.ValidateChoice(Compatibility.TestedChoice with { FullVersion = "1.3.1.234.G59d6bf59" }, false);
        Compatibility.ValidateChoice(Compatibility.LegacyChoice with { FullVersion = "1.2.93.667.G7B5cc0ce" }, false);
    }

    [Theory]
    [InlineData("1.2.70.407.g122a4669", "legacy")]
    [InlineData("1.2.95.453.g0eeebbed", "legacy")]
    [InlineData("1.2.93.667.g7b5cc0ce", "legacy")]
    [InlineData("1.2.96.518.g366879e1", "current")]
    [InlineData("1.3.1.234.g59d6bf59", "current")]
    public void KitForPicksTheKitWhoseRangeContainsTheVersion(string version, string kit)
    {
        Assert.Equal(kit, Compatibility.KitFor(version)!.Id);
    }

    [Fact]
    public void VersionsBelowTheEarliestKitHaveNoKitAndTheNewestKitIsCurrent()
    {
        Assert.Null(Compatibility.KitFor("1.2.40.599.g606b7f29"));
        Assert.Same(Compatibility.Kits[^1], Compatibility.CurrentKit);
        Assert.Same(Compatibility.Kits[0], Compatibility.LegacyKit);
        // Kits are ordered by ascending floor, so KitFor can walk them and keep the last match.
        for (var i = 1; i < Compatibility.Kits.Count; i++)
            Assert.True(Compatibility.Kits[i].Floor > Compatibility.Kits[i - 1].Floor);
        Assert.Equal("Current", Compatibility.MethodOf(Compatibility.TestedVersion));
        Assert.Equal("Current", Compatibility.MethodOf(null));
        Assert.Equal("Legacy", Compatibility.MethodOf(Compatibility.LegacyVersion));
        Assert.Equal("Unsupported", Compatibility.MethodOf("1.2.40.599.g606b7f29"));
        Assert.Equal("Current", Compatibility.TestedChoice.Method);
        Assert.Equal("Legacy", Compatibility.LegacyChoice.Method);
        Assert.Equal("Current", SpotifyChoice.Latest.Method);
    }

    [Fact]
    public void EachBundledKitStagesItsOwnDllAndConfigWithTheSharedProxy()
    {
        using var directory = new TemporaryDirectory();
        PatchFiles.Stage(directory.Path, Compatibility.LegacyKit);
        var legacyDll = File.ReadAllBytes(Path.Combine(directory.Path, "blockthespot.dll"));
        var legacyConfig = File.ReadAllText(Path.Combine(directory.Path, "config.ini"));
        Assert.Equal(0x75, legacyDll[6372]);
        Assert.Equal(0x24, legacyDll[6373]);
        Assert.Contains("1.2.93.667", legacyConfig);
        Assert.True(File.Exists(Path.Combine(directory.Path, "chrome_elf.dll")));

        PatchFiles.Stage(directory.Path, Compatibility.CurrentKit);
        var currentDll = File.ReadAllBytes(Path.Combine(directory.Path, "blockthespot.dll"));
        var currentConfig = File.ReadAllText(Path.Combine(directory.Path, "config.ini"));
        Assert.Equal(0x90, currentDll[6372]);
        Assert.Equal(0x90, currentDll[6373]);
        Assert.Contains("1.3.1.234", currentConfig);
        // Only two IAT jumps differ between the kits; everything else is the same binary.
        Assert.Equal(legacyDll.Length, currentDll.Length);
        Assert.Equal(4, legacyDll.Zip(currentDll, (a, b) => a == b ? 0 : 1).Sum());
    }
}
