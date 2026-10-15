package zfs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"zfs-file-history/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFeatureDescriptions(t *testing.T) {
	output, err := os.ReadFile("testdata/zpool-upgrade-v.txt")
	require.NoError(t, err)

	features := parseFeatureDescriptions(string(output))

	require.NotEmpty(t, features)
	assert.Equal(t, PoolFeature{Name: "async_destroy", Description: "Destroy filesystems asynchronously.", ReadOnlyCompatible: true}, features[0])
	assert.Equal(t, PoolFeature{Name: "lz4_compress", Description: "LZ4 compression algorithm support."}, features[2])
	last := features[len(features)-1]
	assert.Equal(t, "physical_rewrite", last.Name, "the legacy versions behind the features are left out")
	for _, feature := range features {
		assert.True(t, isFeatureName(feature.Name), feature.Name)
		assert.NotEmpty(t, feature.Description, feature.Name)
	}
}

func TestParseFeatureStates(t *testing.T) {
	states, unsupported, compatibility := parseFeatureStates("size\t1000\ncompatibility\tgrub2\nfeature@async_destroy\tenabled\n" +
		"feature@lz4_compress\tactive\nfeature@zstd_compress\tdisabled\nunsupported@org.example:y\treadonly\n" +
		"unsupported@com.example:x\tinactive\n")

	assert.Equal(t, map[string]PoolFeatureState{
		"async_destroy": PoolFeatureEnabled,
		"lz4_compress":  PoolFeatureActive,
		"zstd_compress": PoolFeatureDisabled,
	}, states)
	assert.Equal(t, "grub2", compatibility)
	assert.Equal(t, []PoolFeature{
		{Name: "com.example:x", State: PoolFeatureUnsupportedInactive, Unsupported: true},
		{Name: "org.example:y", State: PoolFeatureUnsupportedReadOnly, Unsupported: true, ReadOnlyCompatible: true},
	}, unsupported)

	_, _, compatibility = parseFeatureStates("size\t1000\n")
	assert.Equal(t, "off", compatibility, "older versions of ZFS have no compatibility")
}

func TestCompatibilityFileNames(t *testing.T) {
	assert.Empty(t, compatibilityFileNames("off"))
	assert.Empty(t, compatibilityFileNames("legacy"))
	assert.Equal(t, []string{"grub2"}, compatibilityFileNames("grub2"))
	assert.Equal(t, []string{"openzfs-2.1-linux", "grub2"}, compatibilityFileNames("openzfs-2.1-linux, grub2"))
}

func TestParseCompatibilityFile(t *testing.T) {
	features := parseCompatibilityFile("# Features which are supported by GRUB2\nasync_destroy\nbookmarks, embedded_data # comment\n\tlz4_compress hole_birth\n")

	assert.Equal(t, map[string]bool{"async_destroy": true, "bookmarks": true, "embedded_data": true, "lz4_compress": true, "hole_birth": true}, features)
}

func TestIntersectFeatures(t *testing.T) {
	assert.Nil(t, intersectFeatures(nil), "no files: all features")
	assert.Equal(t, map[string]bool{"b": true}, intersectFeatures([]map[string]bool{{"a": true, "b": true}, {"b": true, "c": true}}))
}

// fakeFeatureCommands answers "zpool upgrade -v" with the fixture and "zpool get" with properties. The kernel module
// lists no features (like one before OpenZFS 2.1) and "zfs version" fails, see fakeModule.
func fakeFeatureCommands(t *testing.T, properties string) {
	descriptions, err := os.ReadFile("testdata/zpool-upgrade-v.txt")
	require.NoError(t, err)
	testutil.Replace(t, &moduleFeaturesDir, filepath.Join(t.TempDir(), "missing"))
	testutil.Replace(t, &runPoolCommand, func(name string, args ...string) (string, error) {
		if name == "zfs" {
			return "", errors.New("unexpected command: zfs " + strings.Join(args, " "))
		}
		switch strings.Join(args, " ") {
		case "upgrade -v":
			return string(descriptions), nil
		case "get -H -o property,value all tank":
			return properties, nil
		}
		return "", errors.New("unexpected command: " + strings.Join(args, " "))
	})
}

func TestListPoolFeatures(t *testing.T) {
	testutil.Replace(t, &backend, Backend(System{}))
	fakeFeatureCommands(t, "compatibility\toff\nfeature@async_destroy\tenabled\nfeature@lz4_compress\tactive\n"+
		"feature@bookmarks\tdisabled\nfeature@large_blocks\tdisabled\n")

	features, err := ListPoolFeatures("tank")

	require.NoError(t, err)
	assert.Equal(t, "off", features.Compatibility)
	assert.Nil(t, features.Compatible)
	var names, reasons []string
	for _, feature := range features.Features {
		names = append(names, feature.Name)
		reasons = append(reasons, feature.Unavailable)
	}
	// the ones that can be enabled first, then the enabled, then the active ones; only the ones the pool knows
	assert.Equal(t, []string{"bookmarks", "large_blocks", "async_destroy", "lz4_compress"}, names)
	assert.Equal(t, []string{"", "", "already enabled", "already enabled and in use"}, reasons)
	assert.True(t, features.Features[0].ReadOnlyCompatible)
	assert.Equal(t, "\"zfs bookmark\" command", features.Features[0].Description)
}

func TestListPoolFeatures_Compatibility(t *testing.T) {
	testutil.Replace(t, &backend, Backend(System{}))
	etc, share := t.TempDir(), t.TempDir()
	testutil.Replace(t, &compatibilityDirs, []string{etc, share})
	require.NoError(t, os.WriteFile(filepath.Join(share, "grub2"), []byte("# GRUB\nbookmarks\nlz4_compress\n"), 0o644))
	properties := "compatibility\tgrub2\nfeature@bookmarks\tdisabled\nfeature@large_blocks\tdisabled\n"
	fakeFeatureCommands(t, properties)

	features, err := ListPoolFeatures("tank")

	require.NoError(t, err)
	require.NoError(t, features.CompatibilityErr)
	assert.Equal(t, "bookmarks", features.Features[0].Name)
	assert.Empty(t, features.Features[0].Unavailable)
	assert.Equal(t, "large_blocks", features.Features[1].Name)
	assert.Equal(t, "not allowed by compatibility=grub2", features.Features[1].Unavailable)

	t.Run("the file of /etc wins", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(etc, "grub2"), []byte("large_blocks\n"), 0o644))
		features, err := ListPoolFeatures("tank")
		require.NoError(t, err)
		assert.Equal(t, "large_blocks", features.Features[0].Name)
		assert.Empty(t, features.Features[0].Unavailable)
		assert.Equal(t, "not allowed by compatibility=grub2", features.Features[1].Unavailable)
	})

	t.Run("several files allow what all of them allow", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(share, "other"), []byte("large_blocks bookmarks\n"), 0o644))
		fakeFeatureCommands(t, strings.Replace(properties, "grub2", "grub2,other", 1))
		features, err := ListPoolFeatures("tank")
		require.NoError(t, err)
		assert.Empty(t, features.Features[0].Unavailable)
		assert.Equal(t, "not allowed by compatibility=grub2,other", features.Features[1].Unavailable)
	})

	t.Run("a missing file allows nothing", func(t *testing.T) {
		fakeFeatureCommands(t, strings.Replace(properties, "grub2", "missing", 1))
		features, err := ListPoolFeatures("tank")
		require.NoError(t, err)
		assert.ErrorContains(t, features.CompatibilityErr, "the compatibility file missing is not in")
		for _, feature := range features.Features {
			assert.Equal(t, "compatibility=missing cannot be read", feature.Unavailable)
		}
	})

	t.Run("legacy allows nothing", func(t *testing.T) {
		fakeFeatureCommands(t, strings.Replace(properties, "grub2", "legacy", 1))
		features, err := ListPoolFeatures("tank")
		require.NoError(t, err)
		for _, feature := range features.Features {
			assert.Equal(t, "not allowed by compatibility=legacy", feature.Unavailable)
		}
	})

	t.Run("no paths", func(t *testing.T) {
		fakeFeatureCommands(t, strings.Replace(properties, "grub2", "../../etc/passwd", 1))
		features, err := ListPoolFeatures("tank")
		require.NoError(t, err)
		assert.ErrorContains(t, features.CompatibilityErr, "invalid compatibility file name")
	})
}

// fakeModule lets the kernel module support the given features (in sysfs, see moduleFeaturesDir), and "zfs version"
// print the given output. Call it after fakeFeatureCommands.
func fakeModule(t *testing.T, version string, features ...string) {
	dir := t.TempDir()
	for _, feature := range features {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "org.openzfs:"+feature), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "org.openzfs:"+feature, "uname"), []byte(feature+"\n"), 0o644))
	}
	testutil.Replace(t, &moduleFeaturesDir, dir)
	zpool := runPoolCommand
	testutil.Replace(t, &runPoolCommand, func(name string, args ...string) (string, error) {
		if name == "zfs" && strings.Join(args, " ") == "version" {
			return version, nil
		}
		return zpool(name, args...)
	})
}

// The zfs tools may be newer than the loaded kernel module, which refuses the features it does not know: they are
// not offered, and the versions tell why.
func TestListPoolFeatures_KernelModule(t *testing.T) {
	testutil.Replace(t, &backend, Backend(System{}))
	fakeFeatureCommands(t, "feature@bookmarks\tdisabled\nfeature@large_blocks\tdisabled\nfeature@async_destroy\tactive\n")
	fakeModule(t, "zfs-2.4.4-1\nzfs-kmod-2.3.0-1\n", "bookmarks", "async_destroy")

	features, err := ListPoolFeatures("tank")

	require.NoError(t, err)
	assert.Equal(t, "zfs-2.4.4-1", features.ToolsVersion)
	assert.Equal(t, "zfs-kmod-2.3.0-1", features.ModuleVersion)
	assert.True(t, features.ModuleLacksFeatures())
	assert.Equal(t, "bookmarks", features.Features[0].Name)
	assert.Empty(t, features.Features[0].Unavailable)
	assert.Equal(t, "large_blocks", features.Features[1].Name)
	assert.Equal(t, "not supported by the loaded kernel module", features.Features[1].Unavailable)

	t.Run("not known: all are offered", func(t *testing.T) {
		testutil.Replace(t, &moduleFeaturesDir, filepath.Join(t.TempDir(), "missing"))
		features, err := ListPoolFeatures("tank")
		require.NoError(t, err)
		assert.False(t, features.ModuleLacksFeatures())
		assert.Empty(t, features.Features[1].Unavailable)
	})
}

// Features of the pool the installed ZFS does not know (e.g. enabled by a newer ZFS) are listed last, with what they
// mean for the pool.
func TestListPoolFeatures_Unsupported(t *testing.T) {
	testutil.Replace(t, &backend, Backend(System{}))
	fakeFeatureCommands(t, "unsupported@org.example:new\treadonly\nfeature@bookmarks\tdisabled\n"+
		"unsupported@com.example:newer\tinactive\nfeature@async_destroy\tactive\n")

	features, err := ListPoolFeatures("tank")

	require.NoError(t, err)
	var names, reasons []string
	for _, feature := range features.Features {
		names = append(names, feature.Name)
		reasons = append(reasons, feature.Unavailable)
	}
	assert.Equal(t, []string{"bookmarks", "async_destroy", "com.example:newer", "org.example:new"}, names)
	assert.Equal(t, []string{"", "already enabled and in use",
		"not supported by the installed ZFS, enabled but not in use",
		"not supported by the installed ZFS, in use: the pool can only be imported read-only"}, reasons)
	assert.False(t, features.ModuleLacksFeatures())
}

func TestParseZfsVersion(t *testing.T) {
	tools, module := parseZfsVersion("zfs-2.4.4-1\nzfs-kmod-2.4.4-1\n")
	assert.Equal(t, "zfs-2.4.4-1", tools)
	assert.Equal(t, "zfs-kmod-2.4.4-1", module)
}

func TestListPoolFeatures_InvalidPool(t *testing.T) {
	_, err := ListPoolFeatures("-x")
	assert.ErrorContains(t, err, "invalid pool name")
}

func TestEnablePoolFeatures(t *testing.T) {
	fake := setupFakePoolCommands(t, nil, nil)

	require.NoError(t, EnablePoolFeatures("tank", []string{"bookmarks", "large_blocks"}))

	assert.Equal(t, []string{"zpool set feature@bookmarks=enabled tank", "zpool set feature@large_blocks=enabled tank"}, fake.commands)
}

func TestEnablePoolFeatures_InvalidNames(t *testing.T) {
	fake := setupFakePoolCommands(t, nil, nil)

	for _, features := range [][]string{nil, {"bookmarks", "x=enabled readonly=on"}, {"-o"}, {""}, {"Bookmarks"}} {
		assert.Error(t, EnablePoolFeatures("tank", features), "%q", features)
	}
	assert.Error(t, EnablePoolFeatures("-tank", []string{"bookmarks"}))
	assert.Empty(t, fake.commands, "nothing run, not even the valid ones")
}

func TestEnablePoolFeatures_StopsAtTheFirstError(t *testing.T) {
	fake := setupFakePoolCommands(t, nil, nil)
	testutil.Replace(t, &runPoolCommand, func(name string, args ...string) (string, error) {
		fake.commands = append(fake.commands, name+" "+strings.Join(args, " "))
		if strings.Contains(args[1], "large_blocks") {
			return "", errors.New("property 'feature@large_blocks' cannot be set")
		}
		return "", nil
	})

	err := EnablePoolFeatures("tank", []string{"bookmarks", "large_blocks", "lz4_compress"})

	assert.EqualError(t, err, "enabled bookmarks, but cannot enable the feature large_blocks on tank: property 'feature@large_blocks' cannot be set")
	assert.Len(t, fake.commands, 2)
}

func TestEnablePoolFeatures_NeedsRoot(t *testing.T) {
	setupFakePoolCommands(t, errors.New("cannot set property for 'tank': permission denied"), errors.New("sudo: a password is required"))

	err := EnablePoolFeatures("tank", []string{"bookmarks", "large_blocks"})

	needsRoot, ok := errors.AsType[*PoolActionNeedsRootError](err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, "enable the features bookmarks, large_blocks", needsRoot.Action.Name)
	assert.Equal(t, "Enabled the features bookmarks, large_blocks", needsRoot.Action.Done)
	assert.Equal(t, [][]string{
		{ZpoolPath(), "set", "feature@bookmarks=enabled", "tank"},
		{ZpoolPath(), "set", "feature@large_blocks=enabled", "tank"},
	}, needsRoot.Commands())
	assert.False(t, needsRoot.AllowedBySetup(), "the setup does not allow features")
	assert.NotContains(t, err.Error(), SetupPoolActionsHint)
}

func TestPoolActionNeedsRootError_AllowedBySetup(t *testing.T) {
	for _, action := range PoolActions {
		assert.True(t, (&PoolActionNeedsRootError{Action: action, Pool: "tank"}).AllowedBySetup(), action.Name)
	}
	assert.False(t, (&PoolActionNeedsRootError{Action: PoolActionScrub, Then: []PoolAction{PoolActionEnableFeature("bookmarks")}}).AllowedBySetup())
}
