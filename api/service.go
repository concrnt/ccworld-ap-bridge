package api

import (
    "crypto/rand"
    "log"
    "strings"
    "errors"
    "context"
    "encoding/json"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"

	"github.com/totegamma/concurrent/core"
    "github.com/concrnt/ccworld-ap-bridge/types"
    "github.com/concrnt/ccworld-ap-bridge/store"
)

type Service struct {
    store store.Store
    config core.Config
}

func (s *Service) GetPerson(ctx context.Context, id string) (types.ApPerson, error) {
	ctx, span := tracer.Start(ctx, "Api.Service.GetPerson")
	defer span.End()

	person, err := s.store.GetPersonByID(ctx, id)
	if err != nil {
		span.RecordError(err)
        return types.ApPerson{}, err
	}

    return person, nil
}

func (s *Service) UpdatePerson(ctx context.Context, requester string, person types.ApPerson) (types.ApPerson, error) {
    ctx, span := tracer.Start(ctx, "Api.Service.UpdatePerson")
    defer span.End()

	entity, err := s.store.GetEntityByCCID(ctx, requester)
	if err != nil {
		span.RecordError(err)
        return types.ApPerson{}, err
	}

	if entity.CCID != requester {
        return types.ApPerson{}, errors.New("unauthorized")
	}


	created, err := s.store.UpsertPerson(ctx, person)
	if err != nil {
		span.RecordError(err)
        return types.ApPerson{}, err
	}

    return created, nil
}

func (s *Service) Follow(ctx context.Context, requester, targetID string) (types.ApFollow, error) {
    ctx, span := tracer.Start(ctx, "Api.Service.Follow")
    defer span.End()

	entity, err := s.store.GetEntityByCCID(ctx, requester)
	if err != nil {
		span.RecordError(err)
        return types.ApFollow{}, err
	}

	targetActor, err := ResolveActor(ctx, targetID)
	if err != nil {
		log.Println("resolve actor error", err)
		span.RecordError(err)
        return types.ApFollow{}, err
	}

	targetPerson, err := h.FetchPerson(ctx, targetActor, entity)
	if err != nil {
		span.RecordError(err)
        return types.ApFollow{}, err
	}

	simpleID := strings.Replace(targetID, "@", "-", -1)
	simpleID = strings.Replace(simpleID, ".", "-", -1)
	followID := "https://" + s.config.FQDN + "/follow/" + entity.ID + "/" + simpleID

	followObject := types.ApObject{
		Context: "https://www.w3.org/ns/activitystreams",
		Type:    "Follow",
		Actor:   "https://" + s.config.FQDN + "/ap/acct/" + entity.ID,
		Object:  targetPerson.ID,
		ID:      followID,
	}

	err = h.PostToInbox(ctx, targetPerson.Inbox, followObject, entity)
	if err != nil {
		log.Println("post to inbox error", err)
		span.RecordError(err)
        return types.ApFollow{}, err
	}

	follow := types.ApFollow{
		ID:                 followID,
		PublisherPersonURL: targetPerson.ID,
		SubscriberUserID:   entity.ID,
	}

	err = h.repo.SaveFollow(ctx, follow)
	if err != nil {
		log.Println("save follow error", err)
		span.RecordError(err)
        return types.ApFollow{}, err
	}

    return follow, nil
}



func (s *Service) UnFollow(ctx context.Context, requester, targetID string) (types.ApFollow, error) {
    ctx, span := tracer.Start(ctx, "Api.Service.UnFollow")
    defer span.End()

	entity, err := s.store.GetEntityByCCID(ctx, requester)
	if err != nil {
		span.RecordError(err)
        return types.ApFollow{}, err
	}

	simpleID := strings.Replace(targetID, "@", "-", -1)
	simpleID = strings.Replace(simpleID, ".", "-", -1)
	followID := "https://" + s.config.FQDN + "/follow/" + entity.ID + "/" + simpleID
	log.Println("unfollow", followID)

	targetActor, err := ResolveActor(ctx, targetID)
	if err != nil {
		span.RecordError(err)
        return types.ApFollow{}, err
	}

	targetPerson, err := h.FetchPerson(ctx, targetActor, entity)
	if err != nil {
		span.RecordError(err)
        return types.ApFollow{}, err
	}

	undoObject := types.ApObject{
		Context: "https://www.w3.org/ns/activitystreams",
		Type:    "Undo",
		Actor:   "https://" + s.config.FQDN + "/ap/acct/" + entity.ID,
		ID:      followID + "/undo",
		Object: types.ApObject{
			Context: "https://www.w3.org/ns/activitystreams",
			Type:    "Follow",
			ID:      followID,
			Actor:   "https://" + s.config.FQDN + "/ap/acct/" + entity.ID,
			Object:  targetPerson.ID,
		},
	}

	// dump undo object
	undoJSON, err := json.Marshal(undoObject)
	if err != nil {
		span.RecordError(err)
        return types.ApFollow{}, err
	}
	log.Println(string(undoJSON))

	err = h.PostToInbox(ctx, targetPerson.Inbox, undoObject, entity)
	if err != nil {
		span.RecordError(err)
        return types.ApFollow{}, err
	}

	deleted, err := s.store.RemoveFollow(ctx, followID)
	if err != nil {
		span.RecordError(err)
        return types.ApFollow{}, err
	}

    return deleted, nil
}


// CreateEntityRequest is a struct for a request to create an entity.
type CreateEntityRequest struct {
	ID                 string `json:"id"`
}

func (s *Service) CreateEntity(ctx context.Context, requester string, request CreateEntityRequest) (types.ApEntity, error) {
    ctx, span := tracer.Start(ctx, "Api.Service.CreateEntity")
    defer span.End()

	// check if entity already exists
	entity, err := s.store.GetEntityByCCID(ctx, requester)
	if err == nil { // Already exists

        entity.Privatekey = ""
        return entity, nil

	} else { // Create

		// RSAキーペアの生成
		privKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}

		// 秘密鍵をPEM形式に変換
		privKeyBytes := x509.MarshalPKCS1PrivateKey(privKey)
		privKeyPEM := pem.EncodeToMemory(
			&pem.Block{
				Type:  "RSA PRIVATE KEY",
				Bytes: privKeyBytes,
			},
		)

		// 公開鍵をPEM形式に変換
		pubKeyBytes, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
		if err != nil {
			panic(err)
		}
		pubKeyPEM := pem.EncodeToMemory(
			&pem.Block{
				Type:  "PUBLIC KEY",
				Bytes: pubKeyBytes,
			},
		)

		created, err := s.store.CreateEntity(ctx, types.ApEntity{
			ID:                 request.ID,
			CCID:               requester,
			Publickey:          string(pubKeyPEM),
			Privatekey:         string(privKeyPEM),
		})
		if err != nil {
			span.RecordError(err)
            return types.ApEntity{}, err
		}

		created.Privatekey = ""
        return created, nil
	}
}


func (s *Service) GetEntityID(ctx context.Context, ccid string) (types.ApEntity, error) {
    ctx, span := tracer.Start(ctx, "Api.Service.GetEntityID")
    defer span.End()

	entity, err := s.store.GetEntityByCCID(ctx, ccid)
	if err != nil {
		span.RecordError(err)
        return types.ApEntity{}, err
	}

	entity.Privatekey = ""

    return entity, nil
}


