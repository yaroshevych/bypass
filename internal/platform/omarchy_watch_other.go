//go:build !linux

package platform

import "context"

func WaitForOmarchyThemeChange(context.Context) bool { return false }
