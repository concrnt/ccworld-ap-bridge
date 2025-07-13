package bridge

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"
	xhtml "golang.org/x/net/html"

	"github.com/concrnt/ccworld-ap-bridge/apclient"
	"github.com/concrnt/ccworld-ap-bridge/store"
	"github.com/concrnt/ccworld-ap-bridge/types"
	"github.com/concrnt/ccworld-ap-bridge/world"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/core"
	commitStore "github.com/concrnt/concrnt/x/store"
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

func htmlToMarkdown(r io.Reader) (string, error) {
	doc, err := xhtml.Parse(r)
	if err != nil {
		return "", err
	}

	// traverse はノード n を受け取り、変換後の文字列を返す再帰関数です。
	var traverse func(n *xhtml.Node) string
	traverse = func(n *xhtml.Node) string {
		var result strings.Builder

		switch n.Type {
		case xhtml.TextNode:
			result.WriteString(n.Data)
		case xhtml.ElementNode:
			switch n.Data {
			case "a":
				var href string
				for _, attr := range n.Attr {
					if attr.Key == "href" {
						href = attr.Val
						break
					}
				}
				result.WriteString("[")
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					result.WriteString(traverse(c))
				}
				result.WriteString(fmt.Sprintf("](%s)", href))
			case "p":
				result.WriteString("\n\n")
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					result.WriteString(traverse(c))
				}
			case "br":
				result.WriteString("\n")
			default:
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					result.WriteString(traverse(c))
				}
			}
		default:
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				result.WriteString(traverse(c))
			}
		}
		return result.String()
	}

	return traverse(doc), nil
}

func (s Service) NoteToMessage(ctx context.Context, object *types.RawApObj, person *types.RawApObj, destStreams []string) (core.Message, error) {

	idUrl, _ := url.Parse(person.MustGetString("id"))
	hostname := idUrl.Hostname()
	actorID := "@" + person.MustGetString("preferredUsername") + "@" + hostname

	isMisskey := true
	content, ok := object.GetString("_misskey_content")
	if !ok {
		isMisskey = false
		rawcontent := object.MustGetString("content")
		if rawcontent != "" {
			var err error
			content, err = htmlToMarkdown(strings.NewReader(rawcontent))
			if err != nil {
				fmt.Println("html to markdown error", err)
				content = rawcontent
			}
			content = strings.Trim(content, "\n")
		}
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

	if len(content) > 4096 {
		content = content[:4096]
	}

	contentWithImage := content
	for _, attachment := range object.MustGetRawSlice("attachment") {
		if attachment.MustGetString("type") == "document" {
			contentWithImage += "\n\n![image](" + attachment.MustGetString("url") + ")"
		}
	}

	flag := ""
	if object.MustGetBool("sensitive") {
		flag = "sensitive"
	}
	if object.MustGetString("summary") != "" {
		flag = object.MustGetString("summary")
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

	var ReplyToMessageID string
	var ReplyToMessageAuthor string
	var RerouteMessageID string
	var RerouteMessageAuthor string

	if object.MustGetString("inReplyTo") != "" {

		if len(content) == 0 {
			return core.Message{}, errors.New("empty content")
		}

		if strings.HasPrefix(object.MustGetString("inReplyTo"), "https://"+s.config.FQDN+"/ap/note/") {
			replyToMessageID := strings.TrimPrefix(object.MustGetString("inReplyTo"), "https://"+s.config.FQDN+"/ap/note/")
			message, err := s.client.GetMessage(ctx, replyToMessageID, &client.Options{Resolver: s.config.FQDN})
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
						Username: username,
						Avatar:   person.MustGetString("icon.url"),
						Link:     person.MustGetString("url"),
					},
					Flag:                 flag,
					Emojis:               &emojis,
					ReplyToMessageID:     ReplyToMessageID,
					ReplyToMessageAuthor: ReplyToMessageAuthor,
				},
				Meta: map[string]any{
					"apActorId":        actorID,
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

		if len(content) == 0 {
			return core.Message{}, errors.New("empty content")
		}

		if strings.HasPrefix(object.MustGetString("quoteUrl"), "https://"+s.config.FQDN+"/ap/note/") {
			replyToMessageID := strings.TrimPrefix(object.MustGetString("quoteUrl"), "https://"+s.config.FQDN+"/ap/note/")
			message, err := s.client.GetMessage(ctx, replyToMessageID, &client.Options{Resolver: s.config.FQDN})
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
						Username: username,
						Avatar:   person.MustGetString("icon.url"),
						Link:     person.MustGetString("url"),
					},
					Flag:                 flag,
					Emojis:               &emojis,
					RerouteMessageID:     RerouteMessageID,
					RerouteMessageAuthor: RerouteMessageAuthor,
				},
				Meta: map[string]any{
					"apActorId":        actorID,
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
			mediaFlag := ""
			if attachment.MustGetBool("sensitive") {
				mediaFlag = "sensitive"
			}

			mediaType := attachment.MustGetString("mediaType")
			if mediaType == "" {
				mediaType = "image/png"
			}

			media = append(media, world.Media{
				MediaURL:  attachment.MustGetString("url"),
				MediaType: mediaType,
				Flag:      mediaFlag,
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
							Username: username,
							Avatar:   person.MustGetString("icon.url"),
							Link:     person.MustGetString("url"),
						},
						Flag:   flag,
						Medias: &media,
						Emojis: &emojis,
					},
					Meta: map[string]any{
						"apActorId":        actorID,
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

			if len(content) == 0 {
				return core.Message{}, errors.New("empty content")
			}

			schema := world.MarkdownMessageSchema
			if isMisskey {
				schema = world.MisskeyMessageSchema
			}

			doc := core.MessageDocument[world.MarkdownMessage]{
				DocumentBase: core.DocumentBase[world.MarkdownMessage]{
					Signer: s.config.ProxyCCID,
					Type:   "message",
					Schema: schema,
					Body: world.MarkdownMessage{
						Body: content,
						ProfileOverride: &world.ProfileOverride{
							Username: username,
							Avatar:   person.MustGetString("icon.url"),
							Link:     person.MustGetString("url"),
						},
						Flag:   flag,
						Emojis: &emojis,
					},
					Meta: map[string]any{
						"apActorId":        actorID,
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

	var assDocument []byte
	if ReplyToMessageID != "" && ReplyToMessageAuthor != "" {
		assDoc := core.AssociationDocument[world.ReplyAssociation]{
			DocumentBase: core.DocumentBase[world.ReplyAssociation]{
				Signer: s.config.ProxyCCID,
				Owner:  ReplyToMessageAuthor,
				Type:   "association",
				Schema: world.ReplyAssociationSchema,
				Body: world.ReplyAssociation{
					MessageID:     created.Content.ID,
					MessageAuthor: created.Content.Author,
					ProfileOverride: &world.ProfileOverride{
						Username: username,
						Avatar:   person.MustGetString("icon.url"),
						Link:     object.MustGetString("actor"),
					},
				},
				SignedAt: date,
			},
			Target:    ReplyToMessageID,
			Timelines: []string{world.UserNotifyStream + "@" + ReplyToMessageAuthor},
		}
		assDocument, err = json.Marshal(assDoc)
		if err != nil {
			return core.Message{}, errors.Wrap(err, "json marshal error")
		}
	}

	if RerouteMessageID != "" && RerouteMessageAuthor != "" {
		assDoc := core.AssociationDocument[world.RerouteAssociation]{
			DocumentBase: core.DocumentBase[world.RerouteAssociation]{
				Signer: s.config.ProxyCCID,
				Owner:  RerouteMessageAuthor,
				Type:   "association",
				Schema: world.RerouteAssociationSchema,
				Body: world.RerouteAssociation{
					MessageID:     created.Content.ID,
					MessageAuthor: created.Content.Author,
					ProfileOverride: &world.ProfileOverride{
						Username: username,
						Avatar:   person.MustGetString("icon.url"),
						Link:     object.MustGetString("actor"),
					},
				},
				SignedAt: date,
			},
			Target:    RerouteMessageID,
			Timelines: []string{world.UserNotifyStream + "@" + RerouteMessageAuthor},
		}
		assDocument, err = json.Marshal(assDoc)
		if err != nil {
			return core.Message{}, errors.Wrap(err, "json marshal error")
		}
	}

	if len(assDocument) > 0 {
		signatureBytes, err := core.SignBytes(assDocument, s.config.ProxyPriv)
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
			Document:  string(assDocument),
			Signature: string(signature),
			Option:    string(option),
		}

		commit, err := json.Marshal(commitObj)
		if err != nil {
			return core.Message{}, errors.Wrap(err, "json marshal error")
		}

		_, err = s.client.Commit(ctx, s.config.FQDN, string(commit), &core.Association{}, nil)
		if err != nil {
			return core.Message{}, err
		}
	}

	return created.Content, nil
}

func (s Service) MessageToNote(ctx context.Context, messageID string) (types.ApObject, error) {
	ctx, span := tracer.Start(ctx, "MessageToNote")
	defer span.End()

	message, err := s.client.GetMessage(ctx, messageID, &client.Options{Resolver: s.config.FQDN})
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

	images := []string{}
	tags := []types.Tag{}

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

	// extract hash tags
	hashTagPattern := regexp.MustCompile(`#[^#\s]+`)
	hashTagMatches := hashTagPattern.FindAllString(text, -1)
	for _, match := range hashTagMatches {
		fmt.Println("found hashtag", match)
		if strings.Contains(match, "@") {

			timelineFQID := strings.TrimPrefix(match, "#")
			split := strings.Split(timelineFQID, "@")
			if len(split) != 2 {
				continue
			}
			timelineDocument, err := s.client.GetTimeline(ctx, timelineFQID, &client.Options{Resolver: split[1]})
			if err != nil {
				span.RecordError(err)
				continue
			}

			var timeline core.TimelineDocument[world.CommunityTimeline]
			err = json.Unmarshal([]byte(timelineDocument.Document), &timeline)
			if err != nil {
				span.RecordError(err)
				continue
			}

			tag := types.Tag{
				Type: "Hashtag",
				Name: timeline.Body.Name,
				// Href: "https://concrnt.world/timelines/" + strings.TrimPrefix(match, "#"),
			}
			tags = append(tags, tag)

			text = strings.ReplaceAll(text, match, "#"+timeline.Body.Name)

		} else {
			tag := types.Tag{
				Type: "Hashtag",
				Name: match,
			}
			tags = append(tags, tag)
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
			tags = append(tags, emoji)
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
			Tag:            tags,
			Attachment:     attachments,
			CC:             []string{},
		}, nil

	} else if document.Schema == world.ReplyMessageSchema { // Reply

		var replyDocument core.MessageDocument[world.ReplyMessage]
		err = json.Unmarshal([]byte(message.Document), &replyDocument)
		if err != nil {
			return types.ApObject{}, errors.New("invalid payload")
		}

		replyAuthor, err := s.client.GetEntity(ctx, replyDocument.Body.ReplyToMessageAuthor, &client.Options{Resolver: s.config.FQDN})
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("entity not found")
		}

		replySource, err := s.client.GetMessage(ctx, replyDocument.Body.ReplyToMessageID, &client.Options{Resolver: replyAuthor.Domain})
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("message not found")
		}

		var sourceDocument core.MessageDocument[world.MarkdownMessage]
		err = json.Unmarshal([]byte(replySource.Document), &sourceDocument)
		if err != nil {
			return types.ApObject{}, errors.New("invalid payload")
		}

		replyMeta, ok := sourceDocument.Meta.(map[string]any)
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
			tags = append(tags, types.Tag{
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
			Tag:            tags,
			Attachment:     attachments,
		}, nil

	} else if document.Schema == world.RerouteMessageSchema { // Boost or Quote

		var rerouteDocument core.MessageDocument[world.RerouteMessage]
		err = json.Unmarshal([]byte(message.Document), &rerouteDocument)
		if err != nil {
			return types.ApObject{}, errors.New("invalid payload")
		}

		rerouteAuthor, err := s.client.GetEntity(ctx, rerouteDocument.Body.RerouteMessageAuthor, &client.Options{Resolver: s.config.FQDN})
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("entity not found")
		}

		rerouteSource, err := s.client.GetMessage(ctx, rerouteDocument.Body.RerouteMessageID, &client.Options{Resolver: rerouteAuthor.Domain})
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("message not found")
		}

		var sourceDocument core.MessageDocument[world.MarkdownMessage]
		err = json.Unmarshal([]byte(rerouteSource.Document), &sourceDocument)
		if err != nil {
			return types.ApObject{}, errors.New("invalid payload")
		}

		rerouteMeta, ok := sourceDocument.Meta.(map[string]any)
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
			CC:             []string{},
		}, nil
	} else {
		return types.ApObject{}, errors.New("invalid schema")
	}
}
