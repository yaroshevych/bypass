package ui

func (m *Model) beginRunPermissionReset() {
	if m.runs == nil {
		return
	}
	clear(m.runs.grants)
	for secrets := range m.runs.replies {
		secrets.Clear()
		delete(m.runs.replies, secrets)
	}
	m.runs.permissionEpoch++
	m.setNotice("session permissions reset", false)
}
