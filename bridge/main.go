package bridge

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"

	"github.com/concrnt/ccworld-ap-bridge/apclient"
	"github.com/concrnt/ccworld-ap-bridge/store"
	"github.com/concrnt/ccworld-ap-bridge/types"
	"github.com/concrnt/ccworld-ap-bridge/world"
	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/core"
)

var tracer = otel.Tracer("bridge")

type Service struct {
	store    *store.Store
	client   client.Client
	apclient *apclient.ApClient
	config   types.ApConfig
}

func NewService(
	store *store.Store,
	client client.Client,
	apclient *apclient.ApClient,
	config types.ApConfig,
) *Service {
	return &Service{
		store,
		client,
		apclient,
		config,
	}
}

func (s Service) NoteToMessage(ctx context.Context, object types.ApObject, person types.ApObject, destStreams []string) (core.Message, error) {

	content := object.Content

	tags, err := types.ParseTags(object.Tag)
	if err != nil {
		tags = []types.Tag{}
	}

	var emojis map[string]world.Emoji = make(map[string]world.Emoji)
	for _, tag := range tags {
		if tag.Type == "Emoji" {
			name := strings.Trim(tag.Name, ":")
			emojis[name] = world.Emoji{
				ImageURL: tag.Icon.URL,
			}
		}
	}

	if len(content) == 0 {
		return core.Message{}, errors.New("empty note")
	}

	if len(content) > 4096 {
		return core.Message{}, errors.New("note too long")
	}

	if object.Sensitive {
		summary := "CW"
		if object.Summary != "" {
			summary = object.Summary
		}
		content = "<details>\n<summary>" + summary + "</summary>\n" + content + "\n</details>"
	}

	username := person.Name
	if len(username) == 0 {
		username = person.PreferredUsername
	}

	date, err := time.Parse(time.RFC3339Nano, object.Published)
	if err != nil {
		date = time.Now()
	}

	var document []byte
	if object.InReplyTo == "" {

		media := []world.Media{}
		for _, attachment := range object.Attachment {
			media = append(media, world.Media{
				MediaURL:  attachment.URL,
				MediaType: attachment.MediaType,
			})
		}

		if len(object.Attachment) > 0 {
			doc := core.MessageDocument[world.MediaMessage]{
				DocumentBase: core.DocumentBase[world.MediaMessage]{
					Signer: s.config.ProxyCCID,
					Type:   "message",
					Schema: world.MediaMessageSchema,
					Body: world.MediaMessage{
						Body: content,
						ProfileOverride: &world.ProfileOverride{
							Username:    username,
							Avatar:      person.Icon.URL,
							Description: person.Summary,
							Link:        person.URL,
						},
						Medias: &media,
						Emojis: &emojis,
					},
					Meta: map[string]interface{}{
						"apActor":          person.URL,
						"apObjectRef":      object.ID,
						"apPublisherInbox": person.Inbox,
					},
					SignedAt: date,
				},
				Timelines: destStreams,
			}
			document, err = json.Marshal(doc)
			if err != nil {
				return core.Message{}, errors.Wrap(err, "json marshal error")
			}
		} else {
			doc := core.MessageDocument[world.MarkdownMessage]{
				DocumentBase: core.DocumentBase[world.MarkdownMessage]{
					Signer: s.config.ProxyCCID,
					Type:   "message",
					Schema: world.MarkdownMessageSchema,
					Body: world.MarkdownMessage{
						Body: content,
						ProfileOverride: &world.ProfileOverride{
							Username:    username,
							Avatar:      person.Icon.URL,
							Description: person.Summary,
							Link:        person.URL,
						},
						Emojis: &emojis,
					},
					Meta: map[string]interface{}{
						"apActor":          person.URL,
						"apObjectRef":      object.ID,
						"apPublisherInbox": person.Inbox,
					},
					SignedAt: date,
				},
				Timelines: destStreams,
			}
			document, err = json.Marshal(doc)
			if err != nil {
				return core.Message{}, errors.Wrap(err, "json marshal error")
			}
		}

	} else {

		for _, attachment := range object.Attachment {
			if attachment.Type == "Document" {
				content += "\n\n![image](" + attachment.URL + ")"
			}
		}

		var ReplyToMessageID string
		var ReplyToMessageAuthor string

		if strings.HasPrefix(object.InReplyTo, "https://"+s.config.FQDN+"/ap/note/") {
			replyToMessageID := strings.TrimPrefix(object.InReplyTo, "https://"+s.config.FQDN+"/ap/note/")
			message, err := s.client.GetMessage(ctx, s.config.FQDN, replyToMessageID, nil)
			if err != nil {
				return core.Message{}, errors.Wrap(err, "message not found")
			}
			ReplyToMessageID = message.ID
			ReplyToMessageAuthor = message.Author
		} else {
			ref, err := s.store.GetApObjectReferenceByApObjectID(ctx, object.InReplyTo)
			if err != nil {
				return core.Message{}, errors.Wrap(err, "object not found")
			}
			ReplyToMessageID = ref.CcObjectID
			ReplyToMessageAuthor = s.config.ProxyCCID
		}

		doc := core.MessageDocument[world.ReplyMessage]{
			DocumentBase: core.DocumentBase[world.ReplyMessage]{
				Signer: s.config.ProxyCCID,
				Type:   "message",
				Schema: world.ReplyMessageSchema,
				Body: world.ReplyMessage{
					Body: content,
					ProfileOverride: &world.ProfileOverride{
						Username:    username,
						Avatar:      person.Icon.URL,
						Description: person.Summary,
						Link:        person.URL,
					},
					Emojis:               &emojis,
					ReplyToMessageID:     ReplyToMessageID,
					ReplyToMessageAuthor: ReplyToMessageAuthor,
				},
				Meta: map[string]interface{}{
					"apActor":          person.URL,
					"apObjectRef":      object.ID,
					"apPublisherInbox": person.Inbox,
				},
				SignedAt: date,
			},
			Timelines: destStreams,
		}
		document, err = json.Marshal(doc)
		if err != nil {
			return core.Message{}, errors.Wrap(err, "json marshal error")
		}
	}

	signatureBytes, err := core.SignBytes(document, s.config.ProxyPriv)
	if err != nil {
		return core.Message{}, errors.Wrap(err, "sign error")
	}

	signature := hex.EncodeToString(signatureBytes)

	commitObj := core.Commit{
		Document:  string(document),
		Signature: string(signature),
	}

	commit, err := json.Marshal(commitObj)
	if err != nil {
		return core.Message{}, errors.Wrap(err, "json marshal error")
	}

	var created core.ResponseBase[core.Message]
	_, err = s.client.Commit(ctx, s.config.FQDN, string(commit), &created, nil)
	if err != nil {
		return core.Message{}, err
	}

	return created.Content, nil
}

func (s Service) MessageToNote(ctx context.Context, messageID string) (types.ApObject, error) {
	ctx, span := tracer.Start(ctx, "MessageToNote")
	defer span.End()

	message, err := s.client.GetMessage(ctx, s.config.FQDN, messageID, nil)
	if err != nil {
		span.RecordError(err)
		return types.ApObject{}, errors.New("message not found")
	}

	authorEntity, err := s.store.GetEntityByCCID(ctx, message.Author)
	if err != nil {
		span.RecordError(err)
		return types.ApObject{}, errors.New("entity not found")
	}

	var document core.MessageDocument[world.MarkdownMessage]
	err = json.Unmarshal([]byte(message.Document), &document)
	if err != nil {
		return types.ApObject{}, errors.New("invalid payload")
	}

	var emojis []types.Tag
	var images []string

	text := document.Body.Body

	// extract image url of markdown notation
	imagePattern := regexp.MustCompile(`!\[[^]]*\]\(([^)]*)\)`)
	matches := imagePattern.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		images = append(images, match[1])
	}

	// remove markdown notation
	text = imagePattern.ReplaceAllString(text, "")

	if len(*document.Body.Emojis) > 0 {
		for k, v := range *document.Body.Emojis {
			//imageURL, ok := v.(map[string]interface{})["imageURL"].(string)
			emoji := types.Tag{
				ID:   v.ImageURL,
				Type: "Emoji",
				Name: ":" + k + ":",
				Icon: types.Icon{
					Type:      "Image",
					MediaType: "image/png",
					URL:       v.ImageURL,
				},
			}
			emojis = append(emojis, emoji)
		}
	}

	// extract sensitive content
	sensitivePattern := regexp.MustCompile(`<details>((.|\n)*)<summary>((.|\n)*)<\/summary>((.|\n)*)<\/details>`)
	sensitiveMatches := sensitivePattern.FindSubmatch([]byte(text))
	summary := ""
	if len(sensitiveMatches) > 0 {
		//text = string(sensitiveMatches[3]) + "\n" + string(sensitiveMatches[5])
		summary = string(sensitiveMatches[3])
		text = string(sensitiveMatches[5])
	}

	attachments := []types.Attachment{}
	for _, imageURL := range images {
		attachment := types.Attachment{
			Type:      "Document",
			MediaType: "image/png",
			URL:       imageURL,
		}
		attachments = append(attachments, attachment)
	}

	if document.Schema == world.MarkdownMessageSchema { // Note

		return types.ApObject{
			Context:      "https://www.w3.org/ns/activitystreams",
			Type:         "Note",
			ID:           "https://" + s.config.FQDN + "/ap/note/" + message.ID,
			AttributedTo: "https://" + s.config.FQDN + "/ap/acct/" + authorEntity.ID,
			Summary:      summary,
			Content:      text,
			Published:    document.SignedAt.Format(time.RFC3339),
			To:           []string{"https://www.w3.org/ns/activitystreams#Public"},
			Tag:          emojis,
			Attachment:   attachments,
		}, nil

	} else if document.Schema == world.ReplyMessageSchema { // Reply

		var replyDocument core.MessageDocument[world.ReplyMessage]
		err = json.Unmarshal([]byte(message.Document), &replyDocument)
		if err != nil {
			return types.ApObject{}, errors.New("invalid payload")
		}

		replyAuthor, err := s.client.GetEntity(ctx, s.config.FQDN, replyDocument.Body.ReplyToMessageAuthor, nil)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("entity not found")
		}

		replySource, err := s.client.GetMessage(ctx, replyAuthor.Domain, replyDocument.Body.ReplyToMessageID, nil)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("message not found")
		}

		var sourceDocument core.MessageDocument[world.MarkdownMessage]
		err = json.Unmarshal([]byte(replySource.Document), &sourceDocument)
		if err != nil {
			return types.ApObject{}, errors.New("invalid payload")
		}

		replyMeta, ok := sourceDocument.Meta.(map[string]interface{})
		if !ok {
			return types.ApObject{}, errors.New("invalid meta")
		}

		ref, ok := replyMeta["apObjectRef"].(string)
		if !ok {
			ref = "https://" + replyAuthor.Domain + "/ap/note/" + replyDocument.Body.ReplyToMessageID
		}

		return types.ApObject{
			Context:      "https://www.w3.org/ns/activitystreams",
			Type:         "Note",
			ID:           "https://" + s.config.FQDN + "/ap/note/" + message.ID,
			AttributedTo: "https://" + s.config.FQDN + "/ap/acct/" + authorEntity.ID,
			Content:      text,
			InReplyTo:    ref,
			To:           []string{"https://www.w3.org/ns/activitystreams#Public"},
		}, nil

	} else if document.Schema == world.RerouteMessageSchema { // Boost or Quote

		var rerouteDocument core.MessageDocument[world.RerouteMessage]
		err = json.Unmarshal([]byte(message.Document), &rerouteDocument)
		if err != nil {
			return types.ApObject{}, errors.New("invalid payload")
		}

		rerouteAuthor, err := s.client.GetEntity(ctx, s.config.FQDN, rerouteDocument.Body.RerouteMessageAuthor, nil)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("entity not found")
		}

		rerouteSource, err := s.client.GetMessage(ctx, rerouteAuthor.Domain, rerouteDocument.Body.RerouteMessageID, nil)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("message not found")
		}

		var sourceDocument core.MessageDocument[world.MarkdownMessage]
		err = json.Unmarshal([]byte(rerouteSource.Document), &sourceDocument)
		if err != nil {
			return types.ApObject{}, errors.New("invalid payload")
		}

		rerouteMeta, ok := sourceDocument.Meta.(map[string]interface{})
		if !ok {
			return types.ApObject{}, errors.New("invalid meta")
		}

		ref, ok := rerouteMeta["apObjectRef"].(string)
		if !ok {
			ref = "https://" + rerouteAuthor.Domain + "/ap/note/" + rerouteDocument.Body.RerouteMessageID
		}

		if text == "" {
			return types.ApObject{
				Context: "https://www.w3.org/ns/activitystreams",
				Type:    "Announce",
				ID:      "https://" + s.config.FQDN + "/ap/note/" + message.ID,
				Object:  ref,
			}, nil
		}

		return types.ApObject{
			Context:      "https://www.w3.org/ns/activitystreams",
			Type:         "Note",
			ID:           "https://" + s.config.FQDN + "/ap/note/" + message.ID,
			AttributedTo: "https://" + s.config.FQDN + "/ap/acct/" + authorEntity.ID,
			Content:      text,
			QuoteURL:     ref,
			To:           []string{"https://www.w3.org/ns/activitystreams#Public"},
		}, nil
	} else {
		return types.ApObject{}, errors.New("invalid schema")
	}
}
