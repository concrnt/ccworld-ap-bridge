package bridge

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"

	"github.com/concrnt/ccworld-ap-bridge/apclient"
	"github.com/concrnt/ccworld-ap-bridge/store"
	"github.com/concrnt/ccworld-ap-bridge/types"
	"github.com/concrnt/ccworld-ap-bridge/world"
	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/core"
	commitStore "github.com/totegamma/concurrent/x/store"
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

func (s Service) NoteToMessage(ctx context.Context, object *types.RawApObj, person *types.RawApObj, destStreams []string) (core.Message, error) {

	content, ok := object.GetString("_misskey_content")
	if !ok {
		content = object.MustGetString("content")
	}

	tags, _ := object.GetRawSlice("tag")
	var emojis map[string]world.Emoji = make(map[string]world.Emoji)
	for _, tag := range tags {
		if tag.MustGetString("type") == "Emoji" {
			name := strings.Trim(tag.MustGetString("name"), ":")
			emojis[name] = world.Emoji{
				ImageURL: tag.MustGetString("icon.url"),
			}
		}
	}

	if len(content) == 0 {
		return core.Message{}, errors.New("empty note")
	}

	if len(content) > 4096 {
		return core.Message{}, errors.New("note too long")
	}

	contentWithImage := content
	for _, attachment := range object.MustGetRawSlice("attachment") {
		if attachment.MustGetString("type") == "document" {
			contentWithImage += "\n\n![image](" + attachment.MustGetString("url") + ")"
		}
	}

	if object.MustGetBool("sensitive") {
		summary := "CW"
		if object.MustGetString("summary") != "" {
			summary = object.MustGetString("summary")
		}
		content = "<details>\n<summary>" + summary + "</summary>\n" + content + "\n</details>"
		contentWithImage = "<details>\n<summary>" + summary + "</summary>\n" + contentWithImage + "\n</details>"
	}

	username := person.MustGetString("name")
	if len(username) == 0 {
		username = person.MustGetString("preferredUsername")
	}

	date, err := time.Parse(time.RFC3339, object.MustGetString("published"))
	if err != nil {
		date = time.Now()
	}

	to := object.MustGetStringSlice("to")
	cc := object.MustGetStringSlice("cc")

	visibility := "unknown"
	participants := []string{}

	if slices.Contains(to, "https://www.w3.org/ns/activitystreams#Public") || slices.Contains(cc, "https://www.w3.org/ns/activitystreams#Public") {
		visibility = "public"
		goto CHECK_VISIBILITY
	}

	for _, v := range to {
		if strings.HasSuffix(v, "/followers") {
			visibility = "followers"

			follows, err := s.store.GetFollowsByPublisher(ctx, object.MustGetString("attributedTo"))
			if err != nil {
				fmt.Println("followers not found")
				continue
			}
			for _, follow := range follows {
				entity, err := s.store.GetEntityByID(ctx, follow.SubscriberUserID)
				if err != nil {
					fmt.Println("entity not found", err)
					continue
				}
				participants = append(participants, entity.CCID)
			}
		}

		if strings.HasPrefix(v, "https://"+s.config.FQDN+"/ap/acct/") {
			visibility = "direct"
			entity, err := s.store.GetEntityByID(ctx, strings.TrimPrefix(v, "https://"+s.config.FQDN+"/ap/acct/"))
			if err != nil {
				fmt.Println("entity not found")
				continue
			}
			participants = append(participants, entity.CCID)
			break
		}
	}
CHECK_VISIBILITY:

	if visibility == "unknown" {
		return core.Message{}, errors.New("invalid to")
	} else if visibility != "public" && len(participants) == 0 {
		return core.Message{}, errors.New("invalid to")
	}

	var policy = ""
	var policyParams = ""
	if len(participants) > 0 {
		policy = "https://policy.concrnt.world/m/whisper.json"
		params := world.WhisperPolicy{
			Participants: participants,
		}
		policyParamsBytes, err := json.Marshal(params)
		if err != nil {
			return core.Message{}, errors.Wrap(err, "json marshal error")
		}

		policyParams = string(policyParamsBytes)
	}

	var document []byte
	if object.MustGetString("inReplyTo") != "" {

		var ReplyToMessageID string
		var ReplyToMessageAuthor string

		if strings.HasPrefix(object.MustGetString("inReplyTo"), "https://"+s.config.FQDN+"/ap/note/") {
			replyToMessageID := strings.TrimPrefix(object.MustGetString("inReplyTo"), "https://"+s.config.FQDN+"/ap/note/")
			message, err := s.client.GetMessage(ctx, s.config.FQDN, replyToMessageID, nil)
			if err != nil {
				return core.Message{}, errors.Wrap(err, "message not found")
			}
			ReplyToMessageID = message.ID
			ReplyToMessageAuthor = message.Author
		} else {
			ref, err := s.store.GetApObjectReferenceByApObjectID(ctx, object.MustGetString("inReplyTo"))
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
					Body: contentWithImage,
					ProfileOverride: &world.ProfileOverride{
						Username:    username,
						Avatar:      person.MustGetString("icon.url"),
						Description: person.MustGetString("summary"),
						Link:        person.MustGetString("url"),
					},
					Emojis:               &emojis,
					ReplyToMessageID:     ReplyToMessageID,
					ReplyToMessageAuthor: ReplyToMessageAuthor,
				},
				Meta: map[string]interface{}{
					"apActor":          person.MustGetString("url"),
					"apObjectRef":      object.MustGetString("id"),
					"apPublisherInbox": person.MustGetString("inbox"),
					"visibility":       visibility,
				},
				SignedAt:     date,
				Policy:       policy,
				PolicyParams: policyParams,
			},
			Timelines: destStreams,
		}
		document, err = json.Marshal(doc)
		if err != nil {
			return core.Message{}, errors.Wrap(err, "json marshal error")
		}
	} else if object.MustGetString("quoteUrl") != "" {
		var RerouteMessageID string
		var RerouteMessageAuthor string

		if strings.HasPrefix(object.MustGetString("quoteUrl"), "https://"+s.config.FQDN+"/ap/note/") {
			replyToMessageID := strings.TrimPrefix(object.MustGetString("quoteUrl"), "https://"+s.config.FQDN+"/ap/note/")
			message, err := s.client.GetMessage(ctx, s.config.FQDN, replyToMessageID, nil)
			if err != nil {
				return core.Message{}, errors.Wrap(err, "message not found")
			}
			RerouteMessageID = message.ID
			RerouteMessageAuthor = message.Author
		} else {
			ref, err := s.store.GetApObjectReferenceByApObjectID(ctx, object.MustGetString("quoteUrl"))
			if err != nil {
				return core.Message{}, errors.Wrap(err, "object not found")
			}
			RerouteMessageID = ref.CcObjectID
			RerouteMessageAuthor = s.config.ProxyCCID
		}

		doc := core.MessageDocument[world.RerouteMessage]{
			DocumentBase: core.DocumentBase[world.RerouteMessage]{
				Signer: s.config.ProxyCCID,
				Type:   "message",
				Schema: world.RerouteMessageSchema,
				Body: world.RerouteMessage{
					Body: contentWithImage,
					ProfileOverride: &world.ProfileOverride{
						Username:    username,
						Avatar:      person.MustGetString("icon.url"),
						Description: person.MustGetString("summary"),
						Link:        person.MustGetString("url"),
					},
					Emojis:               &emojis,
					RerouteMessageID:     RerouteMessageID,
					RerouteMessageAuthor: RerouteMessageAuthor,
				},
				Meta: map[string]interface{}{
					"apActor":          person.MustGetString("url"),
					"apObjectRef":      object.MustGetString("id"),
					"apPublisherInbox": person.MustGetString("inbox"),
					"visibility":       visibility,
				},
				SignedAt:     date,
				Policy:       policy,
				PolicyParams: policyParams,
			},
			Timelines: destStreams,
		}
		document, err = json.Marshal(doc)
		if err != nil {
			return core.Message{}, errors.Wrap(err, "json marshal error")
		}

	} else {
		media := []world.Media{}
		for _, attachment := range object.MustGetRawSlice("attachment") {
			flag := ""
			if attachment.MustGetBool("sensitive") {
				flag = "sensitive"
			}

			mediaType := attachment.MustGetString("mediaType")
			if mediaType == "" {
				mediaType = "image/png"
			}

			media = append(media, world.Media{
				MediaURL:  attachment.MustGetString("url"),
				MediaType: mediaType,
				Flag:      flag,
			})
		}

		if len(object.MustGetRawSlice("attachment")) > 0 {
			doc := core.MessageDocument[world.MediaMessage]{
				DocumentBase: core.DocumentBase[world.MediaMessage]{
					Signer: s.config.ProxyCCID,
					Type:   "message",
					Schema: world.MediaMessageSchema,
					Body: world.MediaMessage{
						Body: content,
						ProfileOverride: &world.ProfileOverride{
							Username:    username,
							Avatar:      person.MustGetString("icon.url"),
							Description: person.MustGetString("summary"),
							Link:        person.MustGetString("url"),
						},
						Medias: &media,
						Emojis: &emojis,
					},
					Meta: map[string]interface{}{
						"apActor":          person.MustGetString("url"),
						"apObjectRef":      object.MustGetString("id"),
						"apPublisherInbox": person.MustGetString("inbox"),
						"visibility":       visibility,
					},
					SignedAt:     date,
					Policy:       policy,
					PolicyParams: policyParams,
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
							Avatar:      person.MustGetString("icon.url"),
							Description: person.MustGetString("summary"),
							Link:        person.MustGetString("url"),
						},
						Emojis: &emojis,
					},
					Meta: map[string]interface{}{
						"apActor":          person.MustGetString("url"),
						"apObjectRef":      object.MustGetString("id"),
						"apPublisherInbox": person.MustGetString("inbox"),
						"visibility":       visibility,
					},
					SignedAt:     date,
					Policy:       policy,
					PolicyParams: policyParams,
				},
				Timelines: destStreams,
			}
			document, err = json.Marshal(doc)
			if err != nil {
				return core.Message{}, errors.Wrap(err, "json marshal error")
			}
		}

	}

	signatureBytes, err := core.SignBytes(document, s.config.ProxyPriv)
	if err != nil {
		return core.Message{}, errors.Wrap(err, "sign error")
	}

	signature := hex.EncodeToString(signatureBytes)

	opt := commitStore.CommitOption{
		IsEphemeral: true,
	}

	option, err := json.Marshal(opt)
	if err != nil {
		return core.Message{}, errors.Wrap(err, "json marshal error")
	}

	commitObj := core.Commit{
		Document:  string(document),
		Signature: string(signature),
		Option:    string(option),
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

	var document core.MessageDocument[world.MediaMessage]
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

	if document.Body.Medias != nil {
		for _, media := range *document.Body.Medias {
			images = append(images, media.MediaURL)
		}
	}

	// remove markdown notation
	text = imagePattern.ReplaceAllString(text, "")

	if document.Body.Emojis != nil {
		for k, v := range *document.Body.Emojis {
			//imageURL, ok := v.(map[string]interface{})["imageURL"].(string)
			emoji := types.Tag{
				ID:   v.ImageURL,
				Type: "Emoji",
				Name: ":" + k + ":",
				Icon: &types.Icon{
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

	// convert markdown to html
	extensions := parser.CommonExtensions | parser.NoEmptyLineBeforeBlock
	p := parser.NewWithExtensions(extensions)
	doc := p.Parse([]byte(text))

	htmlFlags := html.CommonFlags
	opts := html.RendererOptions{Flags: htmlFlags}
	renderer := html.NewRenderer(opts)

	htmlTextBytes := markdown.Render(doc, renderer)
	htmlText := string(htmlTextBytes)
	htmlText = strings.Trim(htmlText, "\n")

	if document.Schema == world.MarkdownMessageSchema || document.Schema == world.MediaMessageSchema { // Note

		return types.ApObject{
			Context: []string{
				"https://www.w3.org/ns/activitystreams",
				"https://misskey-hub.net/ns#_misskey_content",
			},
			Type:           "Note",
			ID:             "https://" + s.config.FQDN + "/ap/note/" + message.ID,
			AttributedTo:   "https://" + s.config.FQDN + "/ap/acct/" + authorEntity.ID,
			Summary:        summary,
			Content:        htmlText,
			MisskeyContent: text,
			Published:      document.SignedAt.Format(time.RFC3339),
			To:             []string{"https://www.w3.org/ns/activitystreams#Public"},
			Tag:            emojis,
			Attachment:     attachments,
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

		CC := []string{}
		replyToActor, ok := replyMeta["apActor"].(string)
		if ok {
			CC = []string{replyToActor}
			emojis = append(emojis, types.Tag{
				Type: "Mention",
				Href: replyToActor,
			})
		}

		return types.ApObject{
			Context: []string{
				"https://www.w3.org/ns/activitystreams",
				"https://misskey-hub.net/ns#_misskey_content",
			},
			Type:           "Note",
			ID:             "https://" + s.config.FQDN + "/ap/note/" + message.ID,
			AttributedTo:   "https://" + s.config.FQDN + "/ap/acct/" + authorEntity.ID,
			Content:        htmlText,
			MisskeyContent: text,
			InReplyTo:      ref,
			To:             []string{"https://www.w3.org/ns/activitystreams#Public"},
			CC:             CC,
			Tag:            emojis,
			Attachment:     attachments,
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
			Context: []string{
				"https://www.w3.org/ns/activitystreams",
				"https://misskey-hub.net/ns#_misskey_content",
			},
			Type:           "Note",
			ID:             "https://" + s.config.FQDN + "/ap/note/" + message.ID,
			AttributedTo:   "https://" + s.config.FQDN + "/ap/acct/" + authorEntity.ID,
			Content:        htmlText,
			MisskeyContent: text,
			QuoteURL:       ref,
			To:             []string{"https://www.w3.org/ns/activitystreams#Public"},
		}, nil
	} else {
		return types.ApObject{}, errors.New("invalid schema")
	}
}
