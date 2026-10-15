package zfs

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// PoolFeatureState is the state of a feature of a pool, the value of its property "feature@<name>".
type PoolFeatureState string

const (
	// PoolFeatureDisabled is a feature that can be enabled (see EnablePoolFeatures).
	PoolFeatureDisabled PoolFeatureState = "disabled"
	// PoolFeatureEnabled is a feature that is enabled but not used yet: it cannot be disabled again.
	PoolFeatureEnabled PoolFeatureState = "enabled"
	// PoolFeatureActive is a feature that is used by the data of the pool.
	PoolFeatureActive PoolFeatureState = "active"
	// PoolFeatureUnsupportedInactive is an enabled feature the installed ZFS does not support (see
	// PoolFeature.Unsupported), which is not in use: the pool can be used as usual.
	PoolFeatureUnsupportedInactive PoolFeatureState = "inactive"
	// PoolFeatureUnsupportedReadOnly is a feature the installed ZFS does not support, which is in use but read-only
	// compatible: the pool can only be imported read-only.
	PoolFeatureUnsupportedReadOnly PoolFeatureState = "readonly"
)

// PoolFeature is a feature the installed ZFS supports, and its state on a pool (see zpool-features(7)).
type PoolFeature struct {
	// Name is the short name, e.g. "lz4_compress"
	Name        string
	Description string
	// ReadOnlyCompatible is whether software without the feature can still import the pool read-only once it is
	// active
	ReadOnlyCompatible bool
	State              PoolFeatureState
	// Unavailable tells why the feature cannot be enabled, e.g. "already enabled", empty if it can (see
	// ListPoolFeatures)
	Unavailable string
	// Unsupported is whether it is a feature of the pool the installed ZFS does not know (zpool lists it as
	// "unsupported@<guid>", e.g. enabled by a newer ZFS): Name is its GUID, it has no description, and the State is
	// PoolFeatureUnsupportedInactive or PoolFeatureUnsupportedReadOnly
	Unsupported bool
}

// PoolFeatures are the features of a pool, see ListPoolFeatures.
type PoolFeatures struct {
	Pool     string
	Features []PoolFeature
	// Compatibility is the value of the property compatibility of the pool: "off", "legacy", or the names of the
	// files that list the features the pool should be limited to, separated by commas (e.g. "grub2"), see
	// zpoolprops(7)
	Compatibility string
	// Compatible are the features allowed by the files of Compatibility (all of them must allow a feature), nil if
	// it is "off" or "legacy"
	Compatible map[string]bool
	// CompatibilityErr tells why the files of Compatibility could not be read, nil if they could
	CompatibilityErr error
	// ModuleFeatures are the features the loaded kernel module supports (by their short names), nil if not known
	// (it lists them in sysfs since OpenZFS 2.1, on Linux): the zfs tools may be newer than the module (e.g. updated,
	// but not rebooted yet), and the module refuses features it does not know
	ModuleFeatures map[string]bool
	// ToolsVersion and ModuleVersion are the versions of the zfs tools and of the loaded kernel module (as "zfs
	// version" prints them, e.g. "zfs-2.4.4-1" and "zfs-kmod-2.4.4-1"), empty if not known
	ToolsVersion  string
	ModuleVersion string
}

const (
	// compatibilityOff limits the features of a pool to none of the compatibility files.
	compatibilityOff = "off"
	// compatibilityLegacy keeps zpool from enabling any feature of a pool.
	compatibilityLegacy = "legacy"
)

// ListPoolFeatures returns the features of the pool, and why each one cannot be enabled (PoolFeature.Unavailable):
// it is enabled already, the loaded kernel module does not support it, or the property compatibility of the pool
// does not allow it. zpool create and upgrade
// keep to that property, "zpool set feature@" does not: a pool limited to the features GRUB reads (compatibility
// "grub2") would no longer boot. The ones that can be enabled come first, the others are grouped by their reason.
// This spawns processes, so it must not be called on the UI thread.
func ListPoolFeatures(pool string) (*PoolFeatures, error) {
	if pool == "" || strings.HasPrefix(pool, "-") {
		return nil, fmt.Errorf("cannot list the features: invalid pool name %q", pool)
	}
	features, err := backend.PoolFeatures(pool)
	if err != nil {
		return nil, fmt.Errorf("cannot list the features of %s: %w", pool, err)
	}
	for i := range features.Features {
		features.Features[i].Unavailable = features.unavailable(features.Features[i])
	}
	// the ones that can be enabled first, in the order of zpool (roughly the order they were added to ZFS)
	slices.SortStableFunc(features.Features, func(a, b PoolFeature) int {
		return cmp.Compare(unavailableRank(a), unavailableRank(b))
	})
	return features, nil
}

// unavailable returns why the feature cannot be enabled on the pool, empty if it can.
func (features *PoolFeatures) unavailable(feature PoolFeature) string {
	switch {
	case feature.Unsupported && feature.State == PoolFeatureUnsupportedReadOnly:
		return "not supported by the installed ZFS, in use: the pool can only be imported read-only"
	case feature.Unsupported && feature.State == PoolFeatureUnsupportedInactive:
		return "not supported by the installed ZFS, enabled but not in use"
	case feature.Unsupported:
		return fmt.Sprintf("not supported by the installed ZFS, unknown state %q", feature.State)
	case feature.State == PoolFeatureActive:
		return "already enabled and in use"
	case feature.State == PoolFeatureEnabled:
		return "already enabled"
	case feature.State != PoolFeatureDisabled:
		return fmt.Sprintf("unknown state %q", feature.State)
	case !features.supportedByModule(feature.Name):
		return "not supported by the loaded kernel module"
	case features.Compatibility == compatibilityLegacy:
		return "not allowed by compatibility=legacy"
	case features.CompatibilityErr != nil:
		return fmt.Sprintf("compatibility=%s cannot be read", features.Compatibility)
	case features.Compatible != nil && !features.Compatible[feature.Name]:
		return fmt.Sprintf("not allowed by compatibility=%s", features.Compatibility)
	}
	return ""
}

// supportedByModule returns whether the loaded kernel module supports the feature, true if that is not known.
func (features *PoolFeatures) supportedByModule(name string) bool {
	return features.ModuleFeatures == nil || features.ModuleFeatures[name]
}

// ModuleLacksFeatures returns whether the loaded kernel module does not support some of the features of the zfs
// tools, see ModuleFeatures.
func (features *PoolFeatures) ModuleLacksFeatures() bool {
	return slices.ContainsFunc(features.Features, func(feature PoolFeature) bool {
		return !feature.Unsupported && !features.supportedByModule(feature.Name)
	})
}

// unavailableRank orders the features: the ones that can be enabled, the disabled ones that cannot be enabled, the
// enabled ones, the active ones, the ones the installed ZFS does not support.
func unavailableRank(feature PoolFeature) int {
	switch {
	case feature.Unavailable == "":
		return 0
	case feature.Unsupported:
		return 4
	case feature.State == PoolFeatureDisabled:
		return 1
	case feature.State == PoolFeatureEnabled:
		return 2
	}
	return 3
}

// parseFeatureDescriptions parses the features "zpool upgrade -v" lists (without states), in the order listed: a
// line with the name (and "(read-only compatible)"), followed by the indented description.
func parseFeatureDescriptions(output string) []PoolFeature {
	var features []PoolFeature
	listed := false
	for line := range strings.SplitSeq(output, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case !listed:
			// the header, up to the line below "FEAT DESCRIPTION"
			listed = strings.HasPrefix(trimmed, "---")
		case trimmed == "":
			// the end of the features, the legacy versions follow
			return features
		case line[0] == ' ' || line[0] == '\t':
			if len(features) > 0 {
				last := &features[len(features)-1]
				last.Description = strings.TrimSpace(last.Description + " " + trimmed)
			}
		default:
			name, rest, _ := strings.Cut(trimmed, " ")
			features = append(features, PoolFeature{
				Name:               name,
				ReadOnlyCompatible: strings.Contains(rest, "(read-only compatible)"),
			})
		}
	}
	return features
}

// parseFeatureStates parses the output of "zpool get -H -o property,value all <pool>": the states of the features
// by their name, the features the installed ZFS does not support (sorted by their GUID), and the property
// compatibility.
func parseFeatureStates(output string) (states map[string]PoolFeatureState, unsupported []PoolFeature, compatibility string) {
	states = map[string]PoolFeatureState{}
	compatibility = compatibilityOff
	for line := range strings.SplitSeq(output, "\n") {
		property, value, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if name, isFeature := strings.CutPrefix(property, "feature@"); isFeature {
			states[name] = PoolFeatureState(value)
		} else if guid, isUnsupported := strings.CutPrefix(property, "unsupported@"); isUnsupported {
			unsupported = append(unsupported, PoolFeature{Name: guid, State: PoolFeatureState(value), Unsupported: true,
				ReadOnlyCompatible: PoolFeatureState(value) == PoolFeatureUnsupportedReadOnly})
		} else if property == "compatibility" {
			compatibility = value
		}
	}
	slices.SortFunc(unsupported, func(a, b PoolFeature) int { return strings.Compare(a.Name, b.Name) })
	return states, unsupported, compatibility
}

// parseZfsVersion parses the output of "zfs version": the versions of the zfs tools and of the loaded kernel module.
func parseZfsVersion(output string) (tools string, module string) {
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "zfs-kmod-"):
			module = line
		case strings.HasPrefix(line, "zfs-"):
			tools = line
		}
	}
	return tools, module
}

// compatibilityFileNames returns the names of the files a value of the property compatibility refers to, none for
// "off" and "legacy".
func compatibilityFileNames(compatibility string) []string {
	if compatibility == compatibilityOff || compatibility == compatibilityLegacy || compatibility == "" {
		return nil
	}
	var names []string
	for name := range strings.SplitSeq(compatibility, ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// parseCompatibilityFile returns the features a compatibility file lists: names separated by white space or commas,
// comments start with "#".
func parseCompatibilityFile(content string) map[string]bool {
	features := map[string]bool{}
	for line := range strings.SplitSeq(content, "\n") {
		line, _, _ = strings.Cut(line, "#")
		for _, name := range strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\r' }) {
			features[name] = true
		}
	}
	return features
}

// intersectFeatures returns the features all sets contain, nil without sets.
func intersectFeatures(sets []map[string]bool) map[string]bool {
	if len(sets) == 0 {
		return nil
	}
	result := map[string]bool{}
	for name := range sets[0] {
		inAll := true
		for _, set := range sets[1:] {
			inAll = inAll && set[name]
		}
		if inAll {
			result[name] = true
		}
	}
	return result
}

// PoolActionEnableFeature enables the feature (its short name) of a pool. It cannot be undone, like
// PoolActionUpgrade, and it is not part of the sudoers rule of the pool actions (see SudoersRulePoolActions).
func PoolActionEnableFeature(feature string) PoolAction {
	return PoolAction{
		Name: "enable the feature " + feature,
		Done: "Enabled the feature " + feature,
		Args: []string{"set", "feature@" + feature + "=enabled"},
	}
}

// isFeatureName returns whether name is the short name of a feature (lowercase letters, digits and underscores), so
// it cannot change the meaning of the zpool command.
func isFeatureName(name string) bool {
	return name != "" && strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789_") == ""
}

// EnablePoolFeatures enables the features (short names) of the pool, one after another ("zpool set" takes one), and
// stops at the first that fails. It cannot be undone. ZFS also enables the features they depend on. Like
// RunPoolAction it runs zpool with "sudo -n" if zpool is denied, which works only as root or while sudo does not ask
// for a password: the PoolActionNeedsRootError has the features left (PoolActionNeedsRootError.Then). This spawns
// processes, so it must not be called on the UI thread.
func EnablePoolFeatures(pool string, features []string) error {
	if pool == "" || strings.HasPrefix(pool, "-") {
		return fmt.Errorf("cannot enable features: invalid pool name %q", pool)
	}
	if len(features) == 0 {
		return fmt.Errorf("cannot enable features on %s: no features chosen", pool)
	}
	for _, feature := range features {
		if !isFeatureName(feature) {
			return fmt.Errorf("cannot enable features on %s: invalid feature name %q", pool, feature)
		}
	}
	for i, feature := range features {
		err := backend.RunPoolAction(PoolActionEnableFeature(feature), pool)
		if err == nil {
			continue
		}
		if needsRoot, ok := errors.AsType[*PoolActionNeedsRootError](err); ok {
			return enableFeaturesNeedsRoot(pool, features[i:], needsRoot.Cause)
		}
		if i > 0 {
			return fmt.Errorf("enabled %s, but cannot enable the feature %s on %s: %w", strings.Join(features[:i], ", "), feature, pool, err)
		}
		return fmt.Errorf("cannot enable the feature %s on %s: %w", feature, pool, err)
	}
	return nil
}

// enableFeaturesNeedsRoot returns the error that enabling the features needs root: its Action names all of them.
func enableFeaturesNeedsRoot(pool string, features []string, cause error) *PoolActionNeedsRootError {
	action := PoolActionEnableFeature(features[0])
	var then []PoolAction
	for _, feature := range features[1:] {
		then = append(then, PoolActionEnableFeature(feature))
	}
	if len(features) > 1 {
		action.Name = "enable the features " + strings.Join(features, ", ")
		action.Done = "Enabled the features " + strings.Join(features, ", ")
	}
	return &PoolActionNeedsRootError{Action: action, Then: then, Pool: pool, Cause: cause}
}
