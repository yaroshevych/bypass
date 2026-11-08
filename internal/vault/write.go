package vault

import (
	"bytes"
	"context"
	"fmt"
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
	case AddEntry, EditEntry, MoveEntry, DeleteEntry, CreateGroup, DeleteGroup, PrepareDeleteGroup, PrepareRecycleBin, AddAttachment, ReplaceAttachment, RemoveAttachment:
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
	if change.NewName != "" && change.Kind != CreateGroup {
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
