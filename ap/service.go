package ap

import (
	"context"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/concrnt/ccworld-ap-bridge/apclient"
	"github.com/concrnt/ccworld-ap-bridge/bridge"
	"github.com/concrnt/ccworld-ap-bridge/store"
	"github.com/concrnt/ccworld-ap-bridge/types"
	"github.com/concrnt/ccworld-ap-bridge/world"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/core"
	"github.com/concrnt/concrnt/util"
	"github.com/concrnt/concrnt/x/jwt"
	commitStore "github.com/concrnt/concrnt/x/store"
	"github.com/totegamma/httpsig"
)

type Service struct {
	store    *store.Store
	client   client.Client
	apclient *apclient.ApClient
	bridge   *bridge.Service
	info     types.NodeInfo
	config   types.ApConfig
}

func createToken(domain, ccid, priv string) (string, error) {
	token, err := jwt.Create(core.JwtClaims{
		JWTID:          uuid.New().String(),
		IssuedAt:       strconv.FormatInt(time.Now().Unix(), 10),
		ExpirationTime: strconv.FormatInt(time.Now().Add(5*time.Minute).Unix(), 10),
		Audience:       domain,
		Issuer:         ccid,
		Subject:        "concrnt",
	}, priv)
	return token, err
}

func NewService(
	store *store.Store,
	client client.Client,
	apclient *apclient.ApClient,
	bridge *bridge.Service,
	info types.NodeInfo,
	config types.ApConfig,
) *Service {
	return &Service{
		store,
		client,
		apclient,
		bridge,
		info,
		config,
	}
}

func (s *Service) HostMeta(ctx context.Context) (string, error) {
	ctx, span := tracer.Start(ctx, "Ap.Service.HostMeta")
	defer span.End()

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<XRD xmlns="http://docs.oasis-open.org/ns/xri/xrd-1.0">
	<Link rel="lrdd" type="application/xrd+xml" template="https://%s/.well-known/webfinger?resource={uri}"/>
</XRD>`, s.config.FQDN), nil
}

func (s *Service) WebFinger(ctx context.Context, resource string) (types.WebFinger, error) {
	ctx, span := tracer.Start(ctx, "Ap.Service.WebFinger")
	defer span.End()

	var username string

	switch {
	case strings.HasPrefix(resource, "acct:"):
		split := strings.Split(strings.TrimPrefix(resource, "acct:"), "@")
		if len(split) != 2 {
			return types.WebFinger{}, errors.New("invalid resource")
		}
		domain := split[1]
		if domain != s.config.FQDN {
			return types.WebFinger{}, errors.New("domain not found")
		}
		username = split[0]

	case strings.HasPrefix(resource, "https://"+s.config.FQDN+"/ap/acct/"):
		username = strings.TrimPrefix(resource, "https://"+s.config.FQDN+"/ap/acct/")

	default:
		return types.WebFinger{}, errors.New("invalid resource")
	}

	_, err := s.store.GetEntityByID(ctx, username)
	if err != nil {
		return types.WebFinger{}, err
	}

	return types.WebFinger{
		Subject: resource,
		Links: []types.WebFingerLink{
			{
				Rel:  "self",
				Type: "application/activity+json",
				Href: "https://" + s.config.FQDN + "/ap/acct/" + username,
			},
		},
	}, nil
}

func (s *Service) NodeInfo(ctx context.Context) (types.NodeInfo, error) {
	_, span := tracer.Start(ctx, "Ap.Service.NodeInfo")
	defer span.End()

	s.info.Metadata.ProxyCCID = s.config.ProxyCCID

	return s.info, nil
}

func (s *Service) NodeInfoWellKnown(ctx context.Context) (types.WellKnown, error) {
	_, span := tracer.Start(ctx, "Ap.Service.NodeInfoWellKnown")
	defer span.End()
	return types.WellKnown{
		Links: []types.WellKnownLink{
			{
				Rel:  "http://nodeinfo.diaspora.software/ns/schema/2.0",
				Href: "https://" + s.config.FQDN + "/ap/nodeinfo/2.0",
			},
		},
	}, nil
}

// -

func (s *Service) GetUserWebURL(ctx context.Context, id string) (string, error) {
	ctx, span := tracer.Start(ctx, "Ap.Service.GetUserWebURL")
	defer span.End()

	entity, err := s.store.GetEntityByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		return "", err
	}
	return "https://concrnt.world/" + entity.CCID, nil
}

func (s *Service) User(ctx context.Context, id string) (types.ApObject, error) {
	ctx, span := tracer.Start(ctx, "Ap.Service.User")
	defer span.End()

	entity, err := s.store.GetEntityByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		return types.ApObject{}, err
	}

	profile, err := s.client.GetProfile(ctx, entity.CCID+"/world.concrnt.p", &client.Options{Resolver: s.config.FQDN})
	if err != nil {
		span.RecordError(err)
		return types.ApObject{}, err
	}

	var profileDocument core.ProfileDocument[world.Profile]
	err = json.Unmarshal([]byte(profile.Document), &profileDocument)
	if err != nil {
		span.RecordError(err)
		return types.ApObject{}, err
	}

	return types.ApObject{
		Context: []string{
			"https://www.w3.org/ns/activitystreams",
			"https://w3id.org/security/v1",
		},
		Type:        "Person",
		ID:          "https://" + s.config.FQDN + "/ap/acct/" + id,
		Inbox:       "https://" + s.config.FQDN + "/ap/acct/" + id + "/inbox",
		Outbox:      "https://" + s.config.FQDN + "/ap/acct/" + id + "/outbox",
		SharedInbox: "https://" + s.config.FQDN + "/ap/inbox",
		Endpoints: &types.PersonEndpoints{
			SharedInbox: "https://" + s.config.FQDN + "/ap/inbox",
		},
		PreferredUsername: id,
		Name:              profileDocument.Body.Username,
		Summary:           profileDocument.Body.Description,
		URL:               "https://" + s.config.FQDN + "/ap/acct/" + id,
		Icon: &types.Icon{
			Type:      "Image",
			MediaType: "image/png",
			URL:       profileDocument.Body.Avatar,
		},
		PublicKey: &types.Key{
			ID:           "https://" + s.config.FQDN + "/ap/acct/" + id + "#main-key",
			Type:         "Key",
			Owner:        "https://" + s.config.FQDN + "/ap/acct/" + id,
			PublicKeyPem: entity.Publickey,
		},
		AlsoKnownAs: entity.AlsoKnownAs,
	}, nil
}

func (s *Service) GetNoteWebURL(ctx context.Context, id string) (string, error) {
	ctx, span := tracer.Start(ctx, "Ap.Service.GetNoteWebURL")
	defer span.End()

	msg, err := s.client.GetMessage(ctx, id, &client.Options{Resolver: s.config.FQDN})
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	return "https://concrnt.world/" + msg.Author + "/" + id, nil
}

func (s *Service) Note(ctx context.Context, id string) (types.ApObject, error) {
	ctx, span := tracer.Start(ctx, "Ap.Service.Note")
	defer span.End()

	note, err := s.bridge.MessageToNote(ctx, id)
	if err != nil {
		span.RecordError(err)
		return types.ApObject{}, err
	}

	return note, nil
}

func (s *Service) Inbox(ctx context.Context, object *types.RawApObj, inboxId string, request *http.Request) (types.ApObject, error) {
	ctx, span := tracer.Start(ctx, "Ap.Service.Inbox")
	defer span.End()

	verifier, err := httpsig.NewVerifier(request)
	if err != nil {
		span.RecordError(err)
		return types.ApObject{}, errors.Wrap(err, "ap/service/inbox NewVerifier")
	}

	keyid := verifier.KeyId()
	if keyid == "" {
		span.RecordError(err)
		return types.ApObject{}, errors.New("ap/service/inbox KeyId not found")
	}

	var recipientEntity *types.ApEntity
	if inboxId != "" {
		recipients, err := s.store.GetEntityByID(ctx, inboxId)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox GetEntityByID: "+inboxId)
		}

		recipientEntity = &recipients
	}

	requester, err := s.apclient.FetchPerson(ctx, keyid, recipientEntity)
	if err != nil {
		span.RecordError(err)
		return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/follow FetchPerson")
	}

	pubkey, _ := requester.GetRaw("publicKey")
	if pubkey == nil {
		span.RecordError(err)
		return types.ApObject{}, errors.New("ap/service/inbox PublicKey not found: " + keyid)
	}
	pemStr := pubkey.MustGetString("publicKeyPem")

	pemBytes := []byte(pemStr)

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		span.RecordError(err)
		return types.ApObject{}, errors.New("ap/service/inbox Decode error")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		span.RecordError(err)
		return types.ApObject{}, errors.Wrap(err, "ap/service/inbox ParsePKIXPublicKey")
	}

	err = verifier.Verify(pub, httpsig.RSA_SHA256)
	if err != nil {
		fmt.Println("Verify error:", err)

		fmt.Println("keyid", keyid)
		fmt.Println("pemStr", pemStr)

		util.JsonPrint("header", request.Header)
		util.JsonPrint("object", object)

		span.RecordError(err)
		return types.ApObject{}, errors.Wrap(err, "ap/service/inbox Verify")
	}

	switch object.MustGetString("type") {
	case "Follow":
		id := inboxId
		if id == "" {
			toStr, ok := object.GetString("to")
			if !ok {
				return types.ApObject{}, errors.New("ap/service/inbox/follow Invalid Follow ID")
			}
			id = strings.TrimPrefix(toStr, "https://"+s.config.FQDN+"/ap/acct/")
		}
		if id == "" {
			return types.ApObject{}, errors.New("ap/service/inbox/follow Invalid Follow ID")
		}

		entity, err := s.store.GetEntityByID(ctx, id)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/follow GetEntityByID")
		}

		requester, err := s.apclient.FetchPerson(ctx, object.MustGetString("actor"), &entity)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/follow FetchPerson")
		}
		accept := types.ApObject{
			Context: "https://www.w3.org/ns/activitystreams",
			ID:      "https://" + s.config.FQDN + "/ap/acct/" + id + "/follows/" + url.PathEscape(requester.MustGetString("id")),
			Type:    "Accept",
			Actor:   "https://" + s.config.FQDN + "/ap/acct/" + id,
			Object:  object.GetData(),
		}

		split := strings.Split(object.MustGetString("object"), "/")
		userID := split[len(split)-1]

		err = s.apclient.PostToInbox(ctx, requester.MustGetString("inbox"), accept, entity)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/follow PostToInbox")
		}

		// check follow already exists
		_, err = s.store.GetFollowerByTuple(ctx, userID, requester.MustGetString("id"))
		if err == nil {
			log.Println("ap/service/inbox/follow follow already exists")
			return types.ApObject{}, nil
		}

		// save follow
		err = s.store.SaveFollower(ctx, types.ApFollower{
			ID:                  object.MustGetString("id"),
			SubscriberInbox:     requester.MustGetString("inbox"),
			SubscriberPersonURL: requester.MustGetString("id"),
			PublisherUserID:     userID,
		})
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/follow SaveFollower")
		}

		return types.ApObject{}, nil

	case "Like":
		likeObject, ok := object.GetString("object")
		if !ok {
			return types.ApObject{}, errors.New("ap/service/inbox/like Invalid Like Object")
		}

		var targetID string
		if strings.HasPrefix(likeObject, "https://"+s.config.FQDN+"/ap/note/") {
			targetID = strings.TrimPrefix(likeObject, "https://"+s.config.FQDN+"/ap/note/")
		} else {
			ref, err := s.store.GetApObjectReferenceByApObjectID(ctx, likeObject)
			if err != nil {
				return types.ApObject{}, nil
			}
			targetID = ref.CcObjectID
		}

		token, err := createToken(s.config.FQDN, s.config.ProxyCCID, s.config.ProxyPriv)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/like CreateToken")
		}
		targetMsg, err := s.client.GetMessage(ctx, targetID, &client.Options{
			Resolver:  s.config.FQDN,
			AuthToken: token,
		})
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/like GetMessage")
		}

		err = s.store.CreateApObjectReference(ctx, types.ApObjectReference{
			ApObjectID: object.MustGetString("id"),
			CcObjectID: "",
		})

		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/like CreateApObjectReference")
		}

		entity, err := s.store.GetEntityByCCID(ctx, targetMsg.Author)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/like GetEntityByCCID")
		}

		person, err := s.apclient.FetchPerson(ctx, object.MustGetString("actor"), &entity)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/like FetchPerson")
		}

		username := person.MustGetString("name")
		if len(username) == 0 {
			username = person.MustGetString("preferredUsername")
		}

		tag, _ := object.GetRaw("tag")

		var document []byte
		if (tag == nil) || (tag.MustGetString("name")[0] != ':') {
			doc := core.AssociationDocument[world.LikeAssociation]{
				DocumentBase: core.DocumentBase[world.LikeAssociation]{
					Signer: s.config.ProxyCCID,
					Owner:  targetMsg.Author,
					Type:   "association",
					Schema: world.LikeAssociationSchema,
					Body: world.LikeAssociation{
						ProfileOverride: &world.ProfileOverride{
							Username:    username,
							Avatar:      person.MustGetString("icon.url"),
							Description: person.MustGetString("summary"),
							Link:        object.MustGetString("actor"),
						},
					},
					Meta: map[string]any{
						"apActor": object.MustGetString("actor"),
					},
					SignedAt: time.Now(),
				},
				Target: targetID,
				Timelines: []string{
					world.UserNotifyStream + "@" + targetMsg.Author,
				},
			}
			document, err = json.Marshal(doc)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/like Marshal")
			}
		} else {
			doc := core.AssociationDocument[world.ReactionAssociation]{
				DocumentBase: core.DocumentBase[world.ReactionAssociation]{
					Signer: s.config.ProxyCCID,
					Owner:  targetMsg.Author,
					Type:   "association",
					Schema: world.ReactionAssociationSchema,
					Body: world.ReactionAssociation{
						Shortcode: tag.MustGetString("name"),
						ImageURL:  tag.MustGetString("icon.url"),
						ProfileOverride: &world.ProfileOverride{
							Username:    username,
							Avatar:      person.MustGetString("icon.url"),
							Description: person.MustGetString("summary"),
							Link:        object.MustGetString("actor"),
						},
					},
					Meta: map[string]any{
						"apActor": object.MustGetString("actor"),
					},
					SignedAt: time.Now(),
				},
				Target:  targetID,
				Variant: tag.MustGetString("icon.url"),
				Timelines: []string{
					world.UserNotifyStream + "@" + targetMsg.Author,
				},
			}
			document, err = json.Marshal(doc)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/like Marshal")
			}
		}

		signatureBytes, err := core.SignBytes(document, s.config.ProxyPriv)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/like SignBytes")
		}

		signature := hex.EncodeToString(signatureBytes)

		opt := commitStore.CommitOption{
			IsEphemeral: true,
		}

		option, err := json.Marshal(opt)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/like Marshal")
		}

		commitObj := core.Commit{
			Document:  string(document),
			Signature: string(signature),
			Option:    string(option),
		}

		commit, err := json.Marshal(commitObj)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/like Marshal")
		}

		var created core.ResponseBase[core.Association]
		_, err = s.client.Commit(ctx, s.config.FQDN, string(commit), &created, nil)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/like Commit")
		}

		// save reference
		err = s.store.UpdateApObjectReference(ctx, types.ApObjectReference{
			ApObjectID: object.MustGetString("id"),
			CcObjectID: created.Content.ID,
		})

		return types.ApObject{}, nil

	case "Create":
		createObject, ok := object.GetRaw("object")
		if !ok {
			return types.ApObject{}, errors.New("ap/service/inbox/create Invalid Create Object")
		}
		createType, ok := createObject.GetString("type")
		if !ok {
			return types.ApObject{}, errors.New("ap/service/inbox/create Invalid Create Object")
		}
		createID, ok := createObject.GetString("id")
		if !ok {
			return types.ApObject{}, errors.New("ap/service/inbox/create Invalid Create Object")
		}
		switch createType {
		case "Note":
			// check if the note is already exists
			_, err := s.store.GetApObjectReferenceByCcObjectID(ctx, createID)
			if err == nil {
				// already exists
				log.Println("ap/service/inbox/create note already exists")
				return types.ApObject{}, nil
			}

			// preserve reference
			err = s.store.CreateApObjectReference(ctx, types.ApObjectReference{
				ApObjectID: createID,
				CcObjectID: "",
			})

			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, nil
			}

			destStreams := []string{}

			var rep types.ApEntity
			// list up to and ccs
			to, _ := createObject.GetStringSlice("to")
			cc, _ := createObject.GetStringSlice("cc")
			for _, recipient := range append(to, cc...) {
				if strings.HasPrefix(recipient, "https://"+s.config.FQDN+"/ap/acct/") {
					recipient = strings.TrimPrefix(recipient, "https://"+s.config.FQDN+"/ap/acct/")
					entity, err := s.store.GetEntityByID(ctx, recipient)
					if err != nil {
						log.Println("ap/service/inbox/create GetEntityByID", err)
						span.RecordError(err)
						continue
					}
					if rep.ID == "" {
						rep = entity
					}
					destStreams = append(destStreams, world.UserApStream+"@"+entity.CCID)
				}
			}

			// list up follows
			follows, err := s.store.GetFollowsByPublisher(ctx, object.MustGetString("actor"))
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/create GetFollowsByPublisher")
			}

			for _, follow := range follows {
				entity, err := s.store.GetEntityByID(ctx, follow.SubscriberUserID)
				if err != nil {
					log.Println("ap/service/inbox/create GetEntityByID", err)
					span.RecordError(err)
					continue
				}
				if rep.ID == "" {
					rep = entity
				}
				destStreams = append(destStreams, world.UserApStream+"@"+entity.CCID)
			}

			if len(destStreams) == 0 {
				log.Println("ap/service/inbox/create No followers")
				return types.ApObject{}, nil
			}

			var uniqDestStreams []string
			uniqMap := map[string]struct{}{}
			for _, destStream := range destStreams {
				if _, ok := uniqMap[destStream]; !ok {
					uniqMap[destStream] = struct{}{}
					uniqDestStreams = append(uniqDestStreams, destStream)
				}
			}

			person, err := s.apclient.FetchPerson(ctx, object.MustGetString("actor"), &rep)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/create FetchPerson")
			}

			created, err := s.bridge.NoteToMessage(ctx, createObject, person, uniqDestStreams)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/create NoteToMessage")
			}

			// save reference
			err = s.store.UpdateApObjectReference(ctx, types.ApObjectReference{
				ApObjectID: createID,
				CcObjectID: created.ID,
			})

			return types.ApObject{}, nil
		default:
			// print request body
			b, err := json.Marshal(object)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/create Marshal")
			}
			log.Println("Unhandled Create Object", string(b))
			return types.ApObject{}, nil
		}

	case "Announce":
		announceObject, ok := object.GetString("object") //object.Object.(string)
		if !ok {
			return types.ApObject{}, errors.New("ap/service/inbox/announce Invalid Announce Object")
		}
		// check if the note is already exists
		_, err := s.store.GetApObjectReferenceByCcObjectID(ctx, object.MustGetString("id"))
		if err == nil {
			// already exists
			log.Println("ap/service/inbox/announce note already exists")
			return types.ApObject{}, nil
		}

		// preserve reference
		err = s.store.CreateApObjectReference(ctx, types.ApObjectReference{
			ApObjectID: object.MustGetString("id"),
			CcObjectID: "",
		})

		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, nil
		}

		// list up follows
		follows, err := s.store.GetFollowsByPublisher(ctx, object.MustGetString("actor"))
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/announce GetFollowsByPublisher")
		}

		var rep types.ApEntity
		destStreams := []string{}
		for _, follow := range follows {
			entity, err := s.store.GetEntityByID(ctx, follow.SubscriberUserID)
			if err != nil {
				log.Println("ap/service/inbox/announce GetEntityByID", err)
				span.RecordError(err)
				continue
			}
			rep = entity
			destStreams = append(destStreams, world.UserApStream+"@"+entity.CCID)
		}

		if len(destStreams) == 0 {
			log.Println("ap/service/inbox/announce No followers")
			return types.ApObject{}, nil
		}

		person, err := s.apclient.FetchPerson(ctx, object.MustGetString("actor"), &rep)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/announce FetchPerson")
		}

		var sourceMessage core.Message

		// import note
		existing, err := s.store.GetApObjectReferenceByApObjectID(ctx, announceObject)
		if err == nil {
			message, err := s.client.GetMessage(ctx, existing.CcObjectID, &client.Options{Resolver: s.config.FQDN})
			if err == nil {
				sourceMessage = message
			}
			log.Println("message not found: ", existing.CcObjectID, err)
			s.store.DeleteApObjectReference(ctx, announceObject)
		} else {
			// fetch note
			note, err := s.apclient.FetchNote(ctx, announceObject, rep)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, err
			}

			// save person
			person, err := s.apclient.FetchPerson(ctx, note.MustGetString("attributedTo"), &rep)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, err
			}

			// save note as concurrent message
			sourceMessage, err = s.bridge.NoteToMessage(ctx, note, person, []string{world.UserHomeStream + "@" + s.config.ProxyCCID})
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, err
			}

			// save reference
			err = s.store.CreateApObjectReference(ctx, types.ApObjectReference{
				ApObjectID: announceObject,
				CcObjectID: sourceMessage.ID,
			})
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, err
			}
		}

		username := person.MustGetString("name")
		if len(username) == 0 {
			username = person.MustGetString("preferredUsername")
		}

		doc := core.MessageDocument[world.RerouteMessage]{
			DocumentBase: core.DocumentBase[world.RerouteMessage]{
				Signer:   s.config.ProxyCCID,
				Type:     "message",
				Schema:   world.RerouteMessageSchema,
				SignedAt: time.Now(),
				Body: world.RerouteMessage{
					RerouteMessageID:     sourceMessage.ID,
					RerouteMessageAuthor: sourceMessage.Author,
					Body:                 object.MustGetString("content"),
					ProfileOverride: &world.ProfileOverride{
						Username:    username,
						Avatar:      person.MustGetString("icon.url"),
						Description: person.MustGetString("summary"),
						Link:        object.MustGetString("actor"),
					},
				},
				Meta: map[string]any{
					"apActor":          person.MustGetString("url"),
					"apObject":         object.MustGetString("id"),
					"apPublisherInbox": person.MustGetString("inbox"),
				},
			},
			Timelines: destStreams,
		}

		document, err := json.Marshal(doc)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/announce Marshal")
		}

		signatureBytes, err := core.SignBytes(document, s.config.ProxyPriv)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/announce SignBytes")
		}

		signature := hex.EncodeToString(signatureBytes)

		opt := commitStore.CommitOption{
			IsEphemeral: true,
		}

		option, err := json.Marshal(opt)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/announce Marshal")
		}

		commitObj := core.Commit{
			Document:  string(document),
			Signature: string(signature),
			Option:    string(option),
		}

		commit, err := json.Marshal(commitObj)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/announce Marshal")
		}

		var created core.ResponseBase[core.Message]
		_, err = s.client.Commit(ctx, s.config.FQDN, string(commit), &created, nil)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/announce Commit")
		}

		// save reference
		err = s.store.UpdateApObjectReference(ctx, types.ApObjectReference{
			ApObjectID: object.MustGetString("id"),
			CcObjectID: created.Content.ID,
		})

		return types.ApObject{}, nil

	case "Accept":
		acceptObject, ok := object.GetRaw("object")
		if !ok {
			return types.ApObject{}, errors.New("ap/service/inbox/accept Invalid Accept Object")
		}
		acceptType, ok := acceptObject.GetString("type")
		if !ok {
			return types.ApObject{}, errors.New("ap/service/inbox/accept Invalid Accept Object")
		}
		switch acceptType {
		case "Follow":
			objectID, ok := acceptObject.GetString("id")
			if !ok {
				return types.ApObject{}, errors.New("ap/service/inbox/accept Invalid Accept Object")
			}
			apFollow, err := s.store.GetFollowByID(ctx, objectID)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/accept GetFollowByID")
			}
			apFollow.Accepted = true

			_, err = s.store.UpdateFollow(ctx, apFollow)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/accept UpdateFollow")
			}

			return types.ApObject{}, nil
		default:
			// print request body
			util.JsonPrint("Unhandled accept object", object)
			return types.ApObject{}, nil

		}

	case "Undo":
		undoObject, ok := object.GetRaw("object")
		if !ok {
			return types.ApObject{}, errors.New("ap/service/inbox/undo Invalid Undo Object")
		}
		undoType, ok := undoObject.GetString("type")
		if !ok {
			return types.ApObject{}, errors.New("ap/service/inbox/undo Invalid Undo Object")
		}
		switch undoType {
		case "Follow":

			remote, ok := undoObject.GetString("actor")
			if !ok {
				return types.ApObject{}, errors.New("ap/service/inbox/undo/follow Invalid Undo Object")
			}

			obj, ok := undoObject.GetString("object")
			if !ok {
				return types.ApObject{}, errors.New("ap/service/inbox/undo/follow Invalid Undo Object")
			}

			local := strings.TrimPrefix(obj, "https://"+s.config.FQDN+"/ap/acct/")

			// check follow already deleted
			_, err := s.store.GetFollowerByTuple(ctx, local, remote)
			if err != nil {
				log.Println("ap/service/inbox/undo/follow follow already undoed", local, remote)
				return types.ApObject{}, nil
			}
			_, err = s.store.RemoveFollower(ctx, local, remote)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/undo/follow RemoveFollower")
			}
			return types.ApObject{}, nil

		case "Like":
			likeID, ok := undoObject.GetString("id")
			if !ok {
				return types.ApObject{}, errors.New("ap/service/inbox/undo/like Invalid Undo Object")
			}
			deleteRef, err := s.store.GetApObjectReferenceByApObjectID(ctx, likeID)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/undo/like GetApObjectReferenceByApObjectID")
			}

			doc := core.DeleteDocument{
				DocumentBase: core.DocumentBase[any]{
					Signer:   s.config.ProxyCCID,
					Type:     "delete",
					SignedAt: time.Now(),
				},
				Target: deleteRef.CcObjectID,
			}

			document, err := json.Marshal(doc)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/undo/like Marshal")
			}

			signatureBytes, err := core.SignBytes(document, s.config.ProxyPriv)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/undo/like SignBytes")
			}

			signature := hex.EncodeToString(signatureBytes)

			opt := commitStore.CommitOption{
				IsEphemeral: true,
			}

			option, err := json.Marshal(opt)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/undo/like Marshal")
			}

			commitObj := core.Commit{
				Document:  string(document),
				Signature: string(signature),
				Option:    string(option),
			}

			commit, err := json.Marshal(commitObj)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/undo/like Marshal")
			}

			_, err = s.client.Commit(ctx, s.config.FQDN, string(commit), nil, nil)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/undo/like Commit")
			}

			err = s.store.DeleteApObjectReference(ctx, deleteRef.ApObjectID)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/undo/like DeleteApObjectReference")
			}
			return types.ApObject{}, nil

		default:
			// print request body
			util.JsonPrint("Unhandled Undo Object", object)
			return types.ApObject{}, nil
		}
	case "Delete":
		deleteObject, ok := object.GetRaw("object")
		if !ok {
			util.JsonPrint("Delete Object", object.GetData())
			return types.ApObject{}, errors.New("ap/service/inbox/delete Invalid Delete Object")
		}
		deleteID, ok := deleteObject.GetString("id")
		if !ok {
			util.JsonPrint("Delete Object", object.GetData())
			return types.ApObject{}, errors.New("ap/service/inbox/delete Invalid Delete Object")
		}

		deleteRef, err := s.store.GetApObjectReferenceByApObjectID(ctx, deleteID)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/delete GetApObjectReferenceByApObjectID")
		}

		doc := core.DeleteDocument{
			DocumentBase: core.DocumentBase[any]{
				Signer:   s.config.ProxyCCID,
				Type:     "delete",
				SignedAt: time.Now(),
			},
			Target: deleteRef.CcObjectID,
		}

		document, err := json.Marshal(doc)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/delete Marshal")
		}

		signatureBytes, err := core.SignBytes(document, s.config.ProxyPriv)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/delete SignBytes")
		}

		signature := hex.EncodeToString(signatureBytes)

		opt := commitStore.CommitOption{
			IsEphemeral: true,
		}

		option, err := json.Marshal(opt)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/delete Marshal")
		}

		commitObj := core.Commit{
			Document:  string(document),
			Signature: string(signature),
			Option:    string(option),
		}

		commit, err := json.Marshal(commitObj)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/delete Marshal")
		}

		_, err = s.client.Commit(ctx, s.config.FQDN, string(commit), nil, nil)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/delete Commit")
		}

		err = s.store.DeleteApObjectReference(ctx, deleteRef.ApObjectID)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.Wrap(err, "ap/service/inbox/delete DeleteApObjectReference")
		}
		return types.ApObject{}, nil

	default:
		// print request body
		util.JsonPrint("Unhandled Activitypub Object", object)
		return types.ApObject{}, nil
	}
}
