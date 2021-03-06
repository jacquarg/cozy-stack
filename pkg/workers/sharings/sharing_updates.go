package sharings

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"runtime"

	"github.com/cozy/cozy-stack/client/auth"
	"github.com/cozy/cozy-stack/pkg/consts"
	"github.com/cozy/cozy-stack/pkg/couchdb"
	"github.com/cozy/cozy-stack/pkg/couchdb/mango"
	"github.com/cozy/cozy-stack/pkg/instance"
	"github.com/cozy/cozy-stack/pkg/jobs"
	"github.com/cozy/cozy-stack/pkg/permissions"
	"github.com/cozy/cozy-stack/pkg/realtime"
	"github.com/cozy/cozy-stack/pkg/vfs"
)

func init() {
	jobs.AddWorker("sharingupdates", &jobs.WorkerConfig{
		Concurrency: runtime.NumCPU(),
		WorkerFunc:  SharingUpdates,
	})
}

var (
	// ErrSharingIDNotUnique is used when several occurences of the same sharing id are found
	ErrSharingIDNotUnique = errors.New("Several sharings with this id found")
	// ErrSharingDoesNotExist is used when the given sharing does not exist.
	ErrSharingDoesNotExist = errors.New("Sharing does not exist")
	// ErrDocumentNotLegitimate is used when a shared document is triggered but
	// not legitimate for this sharing
	ErrDocumentNotLegitimate = errors.New("Triggered illegitimate shared document")
	//ErrRecipientDoesNotExist is used when the given recipient does not exist
	ErrRecipientDoesNotExist = errors.New("Recipient with given ID does not exist")
	// ErrRecipientHasNoURL is used to signal that a recipient has no URL.
	ErrRecipientHasNoURL = errors.New("Recipient has no URL")
	// ErrEventNotSupported is used to signal that the event propagated by the
	// trigger is not supported by this worker.
	ErrEventNotSupported = errors.New("Event not supported")
)

// TriggerEvent describes the fields retrieved after a triggered event
type TriggerEvent struct {
	Event   *EventDoc       `json:"event"`
	Message *SharingMessage `json:"message"`
}

// EventDoc describes the event returned by the trigger
type EventDoc struct {
	Type string `json:"type"`
	Doc  *couchdb.JSONDoc
}

// SharingMessage describes a sharing message
type SharingMessage struct {
	SharingID string           `json:"sharing_id"`
	Rule      permissions.Rule `json:"rule"`
}

// Sharing describes the sharing document structure
type Sharing struct {
	SharingType      string             `json:"sharing_type"`
	Permissions      permissions.Set    `json:"permissions,omitempty"`
	RecipientsStatus []*RecipientStatus `json:"recipients,omitempty"`
	Sharer           Sharer             `json:"sharer,omitempty"`
}

// Sharer gives the share info, only on the recipient side
type Sharer struct {
	URL          string           `json:"url"`
	SharerStatus *RecipientStatus `json:"sharer_status"`
}

// RecipientStatus contains the information about a recipient for a sharing
type RecipientStatus struct {
	Status       string               `json:"status,omitempty"`
	RefRecipient couchdb.DocReference `json:"recipient,omitempty"`
	AccessToken  *auth.AccessToken
}

// SharingUpdates handles shared document updates
func SharingUpdates(ctx context.Context, m *jobs.Message) error {
	domain := ctx.Value(jobs.ContextDomainKey).(string)

	event := &TriggerEvent{}
	err := m.Unmarshal(&event)
	if err != nil {
		return err
	}
	sharingID := event.Message.SharingID
	rule := event.Message.Rule
	docID := event.Event.Doc.M["_id"].(string)

	// Get the sharing document
	i, err := instance.Get(domain)
	if err != nil {
		return err
	}
	var res []Sharing
	err = couchdb.FindDocs(i, consts.Sharings, &couchdb.FindRequest{
		UseIndex: "by-sharing-id",
		Selector: mango.Equal("sharing_id", sharingID),
	}, &res)
	if err != nil {
		return err
	}
	if len(res) < 1 {
		return ErrSharingDoesNotExist
	} else if len(res) > 1 {
		return ErrSharingIDNotUnique
	}
	sharing := &res[0]

	// Check the updated document is legitimate for this sharing
	if err = checkDocument(sharing, docID); err != nil {
		return err
	}
	return sendToRecipients(i, domain, sharing, &rule, docID, event.Event.Type)
}

// checkDocument checks the legitimity of the updated document to be shared
func checkDocument(sharing *Sharing, docID string) error {
	// Check sharing type
	if sharing.SharingType == consts.OneShotSharing {
		return ErrDocumentNotLegitimate
	}
	return nil
}

// sendToRecipients sends the document to the recipient, or sharer
func sendToRecipients(instance *instance.Instance, domain string, sharing *Sharing, rule *permissions.Rule, docID, eventType string) error {
	var recInfos []*RecipientInfo

	if isRecipientSide(sharing) {
		// We are on the recipient side
		recInfos = make([]*RecipientInfo, 1)
		sharerStatus := sharing.Sharer.SharerStatus
		info, err := extractRecipient(instance, sharerStatus)
		if err != nil {
			return err
		}
		recInfos[0] = info
	} else {
		// We are on the sharer side
		recInfos = make([]*RecipientInfo, len(sharing.RecipientsStatus))
		for i, rec := range sharing.RecipientsStatus {
			info, err := extractRecipient(instance, rec)
			if err != nil {
				return err
			}
			recInfos[i] = info
		}
	}

	opts := &SendOptions{
		DocID:      docID,
		DocType:    rule.Type,
		Recipients: recInfos,
		Path:       fmt.Sprintf("/sharings/doc/%s/%s", rule.Type, docID),
	}

	var fileDoc *vfs.FileDoc
	var dirDoc *vfs.DirDoc
	var err error
	if opts.DocType == consts.Files && eventType != realtime.EventDelete {
		opts.Selector = rule.Selector
		opts.Values = rule.Values

		fs := instance.VFS()
		dirDoc, fileDoc, err = fs.DirOrFileByID(docID)
		if err != nil {
			return err
		}

		if dirDoc != nil {
			opts.Type = consts.DirType
		} else {
			opts.Type = consts.FileType
		}
	}

	switch eventType {
	case realtime.EventCreate:
		if opts.Type == consts.FileType {
			return SendFile(instance, opts, fileDoc)
		}
		if opts.Type == consts.DirType {
			return SendDir(opts, dirDoc)
		}
		return SendDoc(instance, opts)

	case realtime.EventUpdate:
		if opts.Type == consts.FileType {
			if fileDoc.Trashed {
				return DeleteDirOrFile(opts)
			}
			return UpdateOrPatchFile(instance, opts, fileDoc)
		}

		if opts.Type == consts.DirType {
			if dirDoc.DirID == consts.TrashDirID {
				return DeleteDirOrFile(opts)
			}
			return PatchDir(opts, dirDoc)
		}
		return UpdateDoc(instance, opts)

	case realtime.EventDelete:
		if opts.DocType == consts.Files {
			// The event "delete" for a file or a directory means that it has
			// been permanently deleted from the filesystem. We don't propagate
			// this event as of now.
			// TODO Do we propagate this event?
			return nil
		}
		return DeleteDoc(opts)

	default:
		return ErrEventNotSupported
	}
}

func extractRecipient(db couchdb.Database, rec *RecipientStatus) (*RecipientInfo, error) {
	recDoc, err := GetRecipient(db, rec.RefRecipient.ID)
	if err != nil {
		return nil, err
	}
	u, scheme, err := ExtractHostAndScheme(recDoc.M["url"].(string))

	if err != nil {
		return nil, err
	}
	info := &RecipientInfo{
		URL:    u,
		Scheme: scheme,
		Token:  rec.AccessToken.AccessToken,
	}
	return info, nil
}

// GetRecipient returns the Recipient stored in database from a given ID
func GetRecipient(db couchdb.Database, recID string) (*couchdb.JSONDoc, error) {
	doc := &couchdb.JSONDoc{}
	err := couchdb.GetDoc(db, consts.Recipients, recID, doc)
	if couchdb.IsNotFoundError(err) {
		err = ErrRecipientDoesNotExist
	}
	return doc, err
}

// ExtractHostAndScheme returns the recipient's host and the scheme
func ExtractHostAndScheme(fullURL string) (string, string, error) {
	if fullURL == "" {
		return "", "", ErrRecipientHasNoURL
	}
	u, err := url.Parse(fullURL)
	if err != nil {
		return "", "", err
	}
	host := u.Host
	scheme := u.Scheme
	if scheme == "" {
		scheme = "https"
	}
	return host, scheme, nil
}

// isRecipientSide is used to determine whether or not we are on the recipient side.
// A sharing is on the recipient side iff:
// - the sharing type is master-master
// - the SharerStatus structure is not nil
func isRecipientSide(sharing *Sharing) bool {
	if sharing.SharingType == consts.MasterMasterSharing {
		if sharing.Sharer.SharerStatus != nil {
			return true
		}
	}
	return false
}
