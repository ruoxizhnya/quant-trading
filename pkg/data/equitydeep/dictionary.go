package equitydeep

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// FieldDef is one entry of the field dictionary's fields list.
type FieldDef struct {
	FieldCode string   `yaml:"field_code"`
	RawNames  []string `yaml:"raw_names"`
	Unit      string   `yaml:"unit"`
	Statement string   `yaml:"statement"`
	Note      string   `yaml:"note"`
}

// Dictionary is the parsed form of contracts/field_dictionary.yaml (contract
// C1-a). It is read-only after loading: the raw_name → field_code mapping is
// declared by the contract, never derived at runtime.
type Dictionary struct {
	Version   int                `yaml:"version"`
	Status    string             `yaml:"status"`
	FrozenAt  string             `yaml:"frozen_at"`
	BaseUnit  string             `yaml:"base_unit"`
	UnitScale map[string]float64 `yaml:"unit_scale"`
	Fields    []FieldDef         `yaml:"fields"`

	byRawName map[string]FieldDef
}

// LoadDictionary reads and parses the field dictionary from a file path. The
// contracts directory is not embedded in the binary (only docs/openapi.yaml
// is), so the path is supplied by the caller.
func LoadDictionary(path string) (*Dictionary, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read field dictionary %s: %w", path, err)
	}
	d, err := ParseDictionary(raw)
	if err != nil {
		return nil, fmt.Errorf("field dictionary %s: %w", path, err)
	}
	return d, nil
}

// ParseDictionary parses and validates the dictionary. Validation covers
// structural integrity only — it does not second-guess the frozen content.
func ParseDictionary(raw []byte) (*Dictionary, error) {
	var d Dictionary
	if err := yaml.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	if err := d.validate(); err != nil {
		return nil, err
	}
	d.byRawName = make(map[string]FieldDef, len(d.Fields))
	for _, f := range d.Fields {
		for _, rawName := range f.RawNames {
			if _, dup := d.byRawName[rawName]; dup {
				return nil, fmt.Errorf("raw name %q is claimed by more than one field_code", rawName)
			}
			d.byRawName[rawName] = f
		}
	}
	return &d, nil
}

func (d *Dictionary) validate() error {
	if len(d.UnitScale) == 0 {
		return fmt.Errorf("unit_scale is empty")
	}
	if _, ok := d.UnitScale[d.BaseUnit]; !ok {
		return fmt.Errorf("base_unit %q is not a unit_scale key", d.BaseUnit)
	}
	if len(d.Fields) == 0 {
		return fmt.Errorf("fields is empty")
	}
	for i, f := range d.Fields {
		if f.FieldCode == "" {
			return fmt.Errorf("fields[%d] has no field_code", i)
		}
		if len(f.RawNames) == 0 {
			return fmt.Errorf("field_code %q has no raw_names", f.FieldCode)
		}
		if f.Unit == "" {
			return fmt.Errorf("field_code %q has no unit", f.FieldCode)
		}
		if _, ok := d.UnitScale[f.Unit]; !ok {
			return fmt.Errorf("field_code %q declares unit %q which is not a unit_scale key", f.FieldCode, f.Unit)
		}
	}
	return nil
}

// LookupRaw resolves a source field name through the explicit whitelist.
// A miss is a legitimate answer: the caller must drop the field and warn.
func (d *Dictionary) LookupRaw(rawName string) (FieldDef, bool) {
	f, ok := d.byRawName[rawName]
	return f, ok
}

// ScaleFor returns the unit_scale multiplier for a unit key.
func (d *Dictionary) ScaleFor(unit string) (float64, bool) {
	scale, ok := d.UnitScale[unit]
	return scale, ok
}

// FieldCodes returns every field_code in the dictionary, sorted.
func (d *Dictionary) FieldCodes() []string {
	codes := make([]string, 0, len(d.Fields))
	for _, f := range d.Fields {
		codes = append(codes, f.FieldCode)
	}
	sort.Strings(codes)
	return codes
}
