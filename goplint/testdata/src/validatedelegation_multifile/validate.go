// SPDX-License-Identifier: MPL-2.0

package validatedelegation_multifile

// Validate for Config — defined in a different file from the struct.
// Delegates to both fields — should NOT be flagged.
func (c *Config) Validate() error {
	if err := c.FieldName.Validate(); err != nil {
		return err
	}
	if err := c.FieldMode.Validate(); err != nil {
		return err
	}
	return nil
}

// Validate for IncompleteConfig — misses FieldMode delegation.
func (c *IncompleteConfig) Validate() error {
	if err := c.FieldName.Validate(); err != nil {
		return err
	}
	// FieldMode.Validate() is missing!
	return nil
}
