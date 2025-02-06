package store

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"gorm.io/gorm"

	"go.opentelemetry.io/otel"

	"github.com/concrnt/ccworld-ap-bridge/types"
)

var tracer = otel.Tracer("store")

// Store is a repository for ActivityPub.
type Store struct {
	db *gorm.DB
}

// NewStore returns a new Store.
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

func (s Store) GetUserSettings(ctx context.Context, id string) (types.ApUserSettings, error) {
	ctx, span := tracer.Start(ctx, "StoreGetUserSettings")
	defer span.End()

	var settings types.ApUserSettings
	result := s.db.WithContext(ctx).Where("cc_id = ?", id).First(&settings)
	return settings, result.Error
}

func (s Store) UpsertUserSettings(ctx context.Context, settings types.ApUserSettings) error {
	ctx, span := tracer.Start(ctx, "StoreUpsertUserSettings")
	defer span.End()

	return s.db.WithContext(ctx).Save(&settings).Error
}

func (s Store) GetAllEntities(ctx context.Context) ([]types.ApEntity, error) {
	ctx, span := tracer.Start(ctx, "StoreGetAllEntities")
	defer span.End()

	var entities []types.ApEntity
	err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&entities).Error
	return entities, err
}

// GetEntityByID returns an entity by ID.
func (s Store) GetEntityByID(ctx context.Context, id string) (types.ApEntity, error) {
	ctx, span := tracer.Start(ctx, "StoreGetEntityByID")
	defer span.End()

	var entity types.ApEntity
	result := s.db.WithContext(ctx).Where("id = ?", id).First(&entity)
	return entity, result.Error
}

// GetEntityByCCID returns an entity by CCiD.
func (s Store) GetEntityByCCID(ctx context.Context, ccid string) (types.ApEntity, error) {
	ctx, span := tracer.Start(ctx, "StoreGetEntityByCCID")
	defer span.End()

	var entity types.ApEntity
	result := s.db.WithContext(ctx).Where("cc_id = ?", ccid).First(&entity)
	return entity, result.Error
}

// CreateEntity creates an entity.
func (s Store) CreateEntity(ctx context.Context, entity types.ApEntity) (types.ApEntity, error) {
	ctx, span := tracer.Start(ctx, "StoreCreateEntity")
	defer span.End()

	result := s.db.WithContext(ctx).Create(&entity)
	return entity, result.Error
}

func (s Store) UpdateEntityAliases(ctx context.Context, id string, aliases []string) (types.ApEntity, error) {
	ctx, span := tracer.Start(ctx, "StoreUpdateEntityAliases")
	defer span.End()

	var entity types.ApEntity
	result := s.db.WithContext(ctx).Where("id = ?", id).First(&entity)
	if result.Error != nil {
		return entity, result.Error
	}

	entity.AlsoKnownAs = aliases
	result = s.db.WithContext(ctx).Save(&entity)
	return entity, result.Error
}

// Save Follower action
func (s *Store) SaveFollower(ctx context.Context, follower types.ApFollower) error {
	ctx, span := tracer.Start(ctx, "StoreSaveFollow")
	defer span.End()

	return s.db.WithContext(ctx).Create(&follower).Error
}

// SaveFollowing saves follow action
func (s *Store) SaveFollow(ctx context.Context, follow types.ApFollow) error {
	ctx, span := tracer.Start(ctx, "StoreSaveFollow")
	defer span.End()

	return s.db.WithContext(ctx).Create(&follow).Error
}

// GetFollows returns owners follows
func (s *Store) GetFollows(ctx context.Context, ownerID string) ([]types.ApFollow, error) {
	ctx, span := tracer.Start(ctx, "StoreGetFollows")
	defer span.End()

	var follows []types.ApFollow
	err := s.db.WithContext(ctx).Where("subscriber_user_id= ?", ownerID).Find(&follows).Error
	return follows, err
}

// GetFollowers returns owners followers
func (s *Store) GetFollowers(ctx context.Context, ownerID string) ([]types.ApFollower, error) {
	ctx, span := tracer.Start(ctx, "StoreGetFollowers")
	defer span.End()

	var followers []types.ApFollower
	err := s.db.WithContext(ctx).Where("publisher_user_id= ?", ownerID).Find(&followers).Error
	return followers, err
}

// GetFollowsByPublisher returns follows by publisher
func (s *Store) GetFollowsByPublisher(ctx context.Context, publisher string) ([]types.ApFollow, error) {
	ctx, span := tracer.Start(ctx, "StoreGetFollowsByPublisher")
	defer span.End()

	var follows []types.ApFollow
	err := s.db.WithContext(ctx).Where("publisher_person_url = ?", publisher).Find(&follows).Error
	return follows, err
}

// GetFollowerByTuple returns follow by tuple
func (s *Store) GetFollowerByTuple(ctx context.Context, local, remote string) (types.ApFollower, error) {
	ctx, span := tracer.Start(ctx, "StoreGetFollowerByTuple")
	defer span.End()

	var follower types.ApFollower
	result := s.db.WithContext(ctx).Where("publisher_user_id = ? AND subscriber_person_url = ?", local, remote).First(&follower)
	return follower, result.Error
}

// GetFollowByID returns follow by ID
func (s *Store) GetFollowByID(ctx context.Context, id string) (types.ApFollow, error) {
	ctx, span := tracer.Start(ctx, "StoreGetFollowByID")
	defer span.End()

	var follow types.ApFollow
	result := s.db.WithContext(ctx).Where("id = ?", id).First(&follow)
	return follow, result.Error
}

// GetFollowerByID returns follower by ID
func (s *Store) GetFollowerByID(ctx context.Context, id string) (types.ApFollower, error) {
	ctx, span := tracer.Start(ctx, "StoreGetFollowerByID")
	defer span.End()

	var follower types.ApFollower
	result := s.db.WithContext(ctx).Where("id = ?", id).First(&follower)
	return follower, result.Error
}

// UpdateFollow updates follow
func (s *Store) UpdateFollow(ctx context.Context, follow types.ApFollow) (types.ApFollow, error) {
	ctx, span := tracer.Start(ctx, "StoreUpdateFollow")
	defer span.End()

	result := s.db.WithContext(ctx).Save(&follow)
	return follow, result.Error
}

// GetAllFollows returns all Followers actions
func (s *Store) GetAllFollowers(ctx context.Context) ([]types.ApFollower, error) {
	ctx, span := tracer.Start(ctx, "StoreGetAllFollows")
	defer span.End()

	var followers []types.ApFollower
	err := s.db.WithContext(ctx).Find(&followers).Error
	return followers, err
}

// Remove Follow action
func (s *Store) RemoveFollow(ctx context.Context, followID string) (types.ApFollow, error) {
	ctx, span := tracer.Start(ctx, "StoreRemoveFollow")
	defer span.End()

	var follow types.ApFollow
	if err := s.db.WithContext(ctx).First(&follow, "id = ?", followID).Error; err != nil {
		return types.ApFollow{}, err
	}
	err := s.db.WithContext(ctx).Where("id = ?", followID).Delete(&types.ApFollow{}).Error
	if err != nil {
		return types.ApFollow{}, err
	}
	return follow, nil
}

// Remove Follower action
func (s *Store) RemoveFollower(ctx context.Context, local, remote string) (types.ApFollower, error) {
	ctx, span := tracer.Start(ctx, "StoreRemoveFollower")
	defer span.End()

	var follower types.ApFollower
	err := s.db.WithContext(ctx).First(&follower, "publisher_user_id = ? AND subscriber_person_url = ?", local, remote).Error
	if err != nil {
		return types.ApFollower{}, err
	}

	err = s.db.WithContext(ctx).Where("publisher_user_id = ? AND subscriber_person_url = ?", local, remote).Delete(&types.ApFollower{}).Error
	if err != nil {
		return types.ApFollower{}, err
	}
	return follower, nil
}

// Createtypes.ApObjectReference creates reference
func (s *Store) CreateApObjectReference(ctx context.Context, reference types.ApObjectReference) error {
	ctx, span := tracer.Start(ctx, "StoreCreatetypes.ApObjectReference")
	defer span.End()

	return s.db.WithContext(ctx).Create(&reference).Error
}

// Updatetypes.ApObjectReference updates reference
func (s *Store) UpdateApObjectReference(ctx context.Context, reference types.ApObjectReference) error {
	ctx, span := tracer.Start(ctx, "StoreUpdatetypes.ApObjectReference")
	defer span.End()

	return s.db.WithContext(ctx).Save(&reference).Error
}

// Gettypes.ApObjectReferenceByApObjectID returns reference by ap object ID
func (s *Store) GetApObjectReferenceByApObjectID(ctx context.Context, apObjectID string) (types.ApObjectReference, error) {
	ctx, span := tracer.Start(ctx, "StoreGettypes.ApObjectReferenceByApObjectID")
	defer span.End()

	var references types.ApObjectReference
	err := s.db.WithContext(ctx).Where("ap_object_id = ?", apObjectID).First(&references).Error
	return references, err
}

// Gettypes.ApObjectReferenceByCcObjectID returns reference by reference
func (s *Store) GetApObjectReferenceByCcObjectID(ctx context.Context, ccObjectID string) (types.ApObjectReference, error) {
	ctx, span := tracer.Start(ctx, "StoreGettypes.ApObjectReferenceByCcObjectID")
	defer span.End()

	var references types.ApObjectReference
	err := s.db.WithContext(ctx).Where("cc_object_id = ?", ccObjectID).First(&references).Error
	return references, err
}

// Deletetypes.ApObjectReference deletes reference by ap object ID
func (s *Store) DeleteApObjectReference(ctx context.Context, ApObjectID string) error {
	ctx, span := tracer.Start(ctx, "StoreDeletetypes.ApObjectReference")
	defer span.End()

	return s.db.WithContext(ctx).Where("ap_object_id = ?", ApObjectID).Delete(&types.ApObjectReference{}).Error
}

func (s *Store) LoadKey(ctx context.Context, entity types.ApEntity) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(entity.Privatekey))
	if block == nil {
		return &rsa.PrivateKey{}, fmt.Errorf("failed to parse PEM block containing the key")
	}

	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return &rsa.PrivateKey{}, fmt.Errorf("failed to parse DER encoded private key: " + err.Error())
	}

	return priv, nil
}
