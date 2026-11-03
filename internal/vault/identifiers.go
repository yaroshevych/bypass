package vault

import "bypass/internal/vaultmodel"

func splitTags(raw string) []string   { return vaultmodel.SplitTags(raw) }
func normalizeUUID(raw string) string { return vaultmodel.NormalizeUUID(raw) }
