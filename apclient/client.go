package apclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/totegamma/httpsig"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/concrnt/ccworld-ap-bridge/store"
	"github.com/concrnt/ccworld-ap-bridge/types"
)

var (
	UserAgent = "ConcrntWorldApBridge/1.0 (Concrnt)"
)

var tracer = otel.Tracer("apclient")

type ApClient struct {
	mc     *memcache.Client
	store  *store.Store
	config types.ApConfig
}

func NewApClient(
	mc *memcache.Client,
	store *store.Store,
	config types.ApConfig,
) *ApClient {
	return &ApClient{
		mc,
		store,
		config,
	}
}

// FetchNote fetches a note from remote ap server.
func (c ApClient) FetchNote(ctx context.Context, noteID string, execEntity types.ApEntity) (*types.RawApObj, error) {
	_, span := tracer.Start(ctx, "FetchNote")
	defer span.End()

	req, err := http.NewRequest("GET", noteID, nil)
	if err != nil {
		return nil, err
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	req.Header.Set("Accept", "application/activity+json")
	req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Host", req.URL.Host)
	client := new(http.Client)

	priv, err := c.store.LoadKey(ctx, execEntity)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	prefs := []httpsig.Algorithm{httpsig.RSA_SHA256}
	digestAlgorithm := httpsig.DigestSha256
	headersToSign := []string{httpsig.RequestTarget, "date", "host"}
	signer, _, err := httpsig.NewSigner(prefs, digestAlgorithm, headersToSign, httpsig.Signature, 0)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	err = signer.SignRequest(priv, "https://"+c.config.FQDN+"/ap/acct/"+execEntity.ID+"#main-key", req, nil)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	note, err := types.LoadAsRawApObj(body)
	if err != nil {
		return note, err
	}

	return note, nil
}

// FetchPerson fetches a person from remote ap server.
func (c ApClient) FetchPerson(ctx context.Context, actor string, execEntity *types.ApEntity) (*types.RawApObj, error) {
	_, span := tracer.Start(ctx, "FetchPerson")
	defer span.End()

	// try cache
	cache, err := c.mc.Get(actor)
	if err == nil {
		person, err := types.LoadAsRawApObj(cache.Value)
		if err == nil {
			return person, nil
		}
	}

	req, err := http.NewRequest("GET", actor, nil)
	if err != nil {
		return nil, err
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	req.Header.Set("Accept", "application/activity+json")
	req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Host", req.URL.Host)
	client := new(http.Client)

	if execEntity != nil {
		priv, err := c.store.LoadKey(ctx, *execEntity)
		if err != nil {
			log.Println(err)
			return nil, err
		}

		prefs := []httpsig.Algorithm{httpsig.RSA_SHA256}
		digestAlgorithm := httpsig.DigestSha256
		headersToSign := []string{httpsig.RequestTarget, "date", "host"}
		signer, _, err := httpsig.NewSigner(prefs, digestAlgorithm, headersToSign, httpsig.Signature, 0)
		if err != nil {
			log.Println(err)
			return nil, err
		}
		err = signer.SignRequest(priv, "https://"+c.config.FQDN+"/ap/acct/"+execEntity.ID+"#main-key", req, nil)
		if err != nil {
			log.Println(err)
			return nil, err
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	person, err := types.LoadAsRawApObj(body)
	if err != nil {
		log.Println(err)
		return person, err
	}

	// cache
	personBytes, err := json.Marshal(person.GetData())
	if err == nil {
		c.mc.Set(&memcache.Item{
			Key:        actor,
			Value:      personBytes,
			Expiration: 1800, // 30 minutes
		})
	}

	return person, nil
}

// ResolveActor resolves an actor from id notation.
func ResolveActor(ctx context.Context, id string) (string, error) {
	_, span := tracer.Start(ctx, "ResolveActor")
	defer span.End()

	if id[0] == '@' {
		id = id[1:]
	}

	split := strings.Split(id, "@")
	if len(split) != 2 {
		return "", fmt.Errorf("invalid id")
	}

	domain := split[1]

	targetlink := "https://" + domain + "/.well-known/webfinger?resource=acct:" + id

	var webfinger types.WebFinger
	req, err := http.NewRequest("GET", targetlink, nil)
	if err != nil {
		return "", err
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	req.Header.Set("Accept", "application/jrd+json")
	req.Header.Set("User-Agent", UserAgent)
	client := new(http.Client)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	err = json.Unmarshal(body, &webfinger)
	if err != nil {
		fmt.Println(string(body))
		return "", err
	}

	var aplink types.WebFingerLink
	for _, link := range webfinger.Links {
		if link.Rel == "self" {
			aplink = link
		}
	}

	if aplink.Href == "" {
		return "", fmt.Errorf("no ap link found")
	}

	return aplink.Href, nil
}

// PostToInbox posts a message to remote ap server.
func (c ApClient) PostToInbox(ctx context.Context, inbox string, object interface{}, entity types.ApEntity) error {

	objectBytes, err := json.Marshal(object)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", inbox, bytes.NewBuffer(objectBytes))
	if err != nil {
		return err
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	req.Header.Set("Content-Type", "application/activity+json")
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	req.Header.Set("Host", req.URL.Host)
	client := new(http.Client)

	priv, err := c.store.LoadKey(ctx, entity)
	if err != nil {
		log.Println(err)
		return err
	}

	prefs := []httpsig.Algorithm{httpsig.RSA_SHA256}
	digestAlgorithm := httpsig.DigestSha256
	headersToSign := []string{httpsig.RequestTarget, "date", "digest", "host"}
	signer, _, err := httpsig.NewSigner(prefs, digestAlgorithm, headersToSign, httpsig.Signature, 0)
	if err != nil {
		log.Println(err)
		return err
	}
	err = signer.SignRequest(priv, "https://"+c.config.FQDN+"/ap/acct/"+entity.ID+"#main-key", req, objectBytes)

	resp, err := client.Do(req)
	if err != nil {
		log.Println(err)
		return err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println(err)
	}
	log.Printf("POST %s [%d]: %s", inbox, resp.StatusCode, string(body))

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("error posting to inbox: %d", resp.StatusCode)
	}

	defer resp.Body.Close()

	return nil
}
