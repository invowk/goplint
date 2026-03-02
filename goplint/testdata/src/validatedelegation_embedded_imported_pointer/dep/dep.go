// SPDX-License-Identifier: MPL-2.0

package dep

import "fmt"

type Child string

func (c Child) Validate() error {
	if c == "" {
		return fmt.Errorf("empty")
	}
	return nil
}
