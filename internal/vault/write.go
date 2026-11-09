package vault

import (
	"bytes"
	"context"
	"fmt"
	"strings"
)

const maxGeneratedPasswordLength = 1024

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrCanceled, err)
	}
	return nil
}

func validateChange(change Change) error {
	switch change.Kind {
	case AddEntry, EditEntry, MoveEntry, MoveGroup, DeleteEntry, CreateGroup, DeleteGroup, PrepareDeleteGroup, PrepareRecycleBin, AddAttachment, ReplaceAttachment, RemoveAttachment, RenameTag, RenameGroup:
	default:
		return ErrUnsupported
	}
	isAttachmentChange := change.Kind == AddAttachment || change.Kind == ReplaceAttachment || change.Kind == RemoveAttachment
	for key := range change.Values {
		if key != "Title" && key != "UserName" && key != "URL" && key != "Notes" && key != "Tags" && !supportedTOTPField(key) {
			return ErrUnsupported
		}
	}
	if len(change.Values) > 0 && change.Kind != AddEntry && change.Kind != EditEntry {
		return ErrUnsupported
	}
	if (change.PasswordChanged || len(change.Password) > 0) && change.Kind != AddEntry && change.Kind != EditEntry {
		return ErrUnsupported
	}
	if len(change.Password) > 0 && !change.PasswordChanged {
		return ErrUnsupported
	}
	if change.NewName != "" && change.Kind != CreateGroup && change.Kind != RenameTag && change.Kind != RenameGroup {
		return ErrUnsupported
	}
	if change.Kind == CreateGroup || change.Kind == RenameGroup {
		if strings.TrimSpace(change.NewName) == "" || strings.ContainsAny(change.NewName, "\r\n\x00") {
			return ErrUnsupported
		}
	}
	if change.Kind == RenameTag {
		if strings.TrimSpace(change.TagName) == "" || strings.TrimSpace(change.NewName) == "" || strings.ContainsAny(change.TagName+change.NewName, ",;\t\r\n\x00") {
			return ErrUnsupported
		}
	} else if change.TagName != "" {
		return ErrUnsupported
	}
	if change.Kind == MoveGroup {
		if change.SourceGroupUUID == "" || change.GroupUUID == "" || change.SourceGroupUUID == change.GroupUUID {
			return ErrUnsupported
		}
	} else if change.SourceGroupUUID != "" {
		return ErrUnsupported
	}
	if isAttachmentChange {
		if !validAttachmentName(change.AttachmentName) || len(change.AttachmentData) > maxAttachmentBytes {
			return ErrUnsupported
		}
		if change.Kind == RemoveAttachment && change.AttachmentData != nil {
			return ErrUnsupported
		}
	} else if change.AttachmentName != "" || change.AttachmentData != nil {
		return ErrUnsupported
	}
	if bytes.ContainsAny(change.Password, "\r\n\x00") {
		return ErrUnsupported
	}
	return nil
}
