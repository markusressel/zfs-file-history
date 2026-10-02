package zfs

import (
	"fmt"
	"strings"
)

// Property is a ZFS property of a dataset ("zfs get"), including user properties like "com.sun:auto-snapshot".
type Property struct {
	Name  string
	Value string
	// Source is where the value comes from: "local", "default", "inherited from <dataset>", "received", "temporary",
	// or "-" for values that cannot be set (statistics like "used", or values fixed at creation like "encryption").
	Source string
}

func (p Property) TableRowId() string {
	return p.Name
}

// IsEditable returns whether the property can be changed with "zfs set".
func (p Property) IsEditable() bool {
	return p.Source != "-"
}

// IsLocal returns whether the value is set on the dataset itself, so "zfs inherit" resets it to the inherited
// (or default) value.
func (p Property) IsLocal() bool {
	return p.Source == "local"
}

// IsUserProperty returns whether this is a user property ("module:property"), which can be set to any value.
func (p Property) IsUserProperty() bool {
	return strings.Contains(p.Name, ":")
}

// ListProperties returns all properties of the dataset, with human-readable values (e.g. "1.5G").
// This spawns a zfs process, so it must not be called on the UI thread.
func ListProperties(dataset string) ([]*Property, error) {
	output, err := runZfs("get", "-H", "-o", "property,value,source", "all", dataset)
	if err != nil {
		return nil, err
	}
	return parseProperties(output)
}

// parseProperties parses the output of "zfs get -H -o property,value,source", e.g.:
//
//	compression	zstd	inherited from rpool
func parseProperties(output string) ([]*Property, error) {
	var properties []*Property
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			return nil, fmt.Errorf("unexpected output of zfs get: %q", line)
		}
		properties = append(properties, &Property{Name: fields[0], Value: fields[1], Source: fields[2]})
	}
	return properties, nil
}

// SetPropertyCommand returns the command that sets the property (without "sudo"), e.g.
// "zfs set compression=zstd pool/data".
func SetPropertyCommand(dataset string, name string, value string) []string {
	return []string{"zfs", "set", name + "=" + value, dataset}
}

// InheritPropertyCommand returns the command that removes the local value of the property (without "sudo"), so the
// inherited or default value applies, e.g. "zfs inherit compression pool/data".
func InheritPropertyCommand(dataset string, name string) []string {
	return []string{"zfs", "inherit", name, dataset}
}

// SetProperty sets the property of the dataset as the current user. If ZFS denies it, the returned error wraps
// ErrPermissionDenied, so the command can be run with sudo instead.
// This spawns a zfs process, so it must not be called on the UI thread.
func SetProperty(dataset string, name string, value string) error {
	if err := validatePropertyName(name); err != nil {
		return err
	}
	if strings.ContainsAny(value, "\n\r") {
		return fmt.Errorf("the value of %s must not contain line breaks", name)
	}
	return runZfsAsUser(SetPropertyCommand(dataset, name, value))
}

// InheritProperty removes the local value of the property, as the current user. If ZFS denies it, the returned
// error wraps ErrPermissionDenied, so the command can be run with sudo instead.
// This spawns a zfs process, so it must not be called on the UI thread.
func InheritProperty(dataset string, name string) error {
	if err := validatePropertyName(name); err != nil {
		return err
	}
	return runZfsAsUser(InheritPropertyCommand(dataset, name))
}

func validatePropertyName(name string) error {
	if name == "" || strings.ContainsAny(name, "= \t\n") {
		return fmt.Errorf("invalid property name: %q", name)
	}
	return nil
}

// runZfsAsUser runs a zfs command (including the leading "zfs") as the current user. If ZFS denies it, the returned
// error wraps ErrPermissionDenied.
func runZfsAsUser(command []string) error {
	if _, err := runZfs(command[1:]...); err != nil {
		if isPermissionDenied(err) {
			return fmt.Errorf("%w: %s", ErrPermissionDenied, err.Error())
		}
		return err
	}
	return nil
}
