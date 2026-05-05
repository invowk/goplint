// SPDX-License-Identifier: MPL-2.0

package cobra

import "context"

type Command struct{}

func (c *Command) Context() context.Context {
	return context.Background()
}
