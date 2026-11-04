package ui

func (m *Model) beginRunPermissionReset() {
	if m.runs == nil {
		return
	}
	clear(m.runs.grants)
	m.runs.permissionEpoch++
	m.setNotice("session permissions reset", false)
}
