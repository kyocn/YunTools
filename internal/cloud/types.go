// Package cloud defines provider-independent capabilities. Credentials never belong in UI logs.
package cloud

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("entry not found")

type Account struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	Region          string `json:"region"`
	AccessKeyID     string `json:"accessKeyId"`
	AccessKeySecret string `json:"accessKeySecret"`
	SecurityToken   string `json:"securityToken,omitempty"`
}

type SecretStore interface {
	Get(string) ([]byte, error)
	Put(string, []byte) error
	Delete(string) error
}

type Bucket struct {
	Name, Region, StorageClass string
	Created                    time.Time
}
type Object struct {
	Key, ETag, VersionID, StorageClass, ContentType, CRC64 string
	Size                                                   int64
	Modified                                               time.Time
	IsDir                                                  bool
	Metadata                                               map[string]string
}
type ObjectPage struct {
	Objects   []Object
	NextToken string
}

// ObjectPageLister optionally supports a caller-selected remote page size.
// Continuation tokens are opaque and valid only for the same bucket and prefix.
type ObjectPageLister interface {
	ListObjectsPage(context.Context, string, string, string, int) (ObjectPage, error)
}
type Progress struct {
	Completed, Total int64
	Stage            string
}
type Transfer struct {
	Bucket, Key, LocalPath, DestinationRoot, CheckpointDir, ETag, VersionID string
	Size                                                                    int64
	Modified                                                                time.Time
}
type ObjectStorage interface {
	ListBuckets(context.Context) ([]Bucket, error)
	CreateBucket(context.Context, string, string) error
	CreateDirectory(context.Context, string, string) error
	DeleteBucket(context.Context, string) error
	ListObjects(context.Context, string, string, string) (ObjectPage, error)
	Stat(context.Context, string, string) (Object, error)
	SetMetadata(context.Context, string, string, map[string]string) error
	CopyObject(context.Context, string, string, string) error
	DeleteObjects(context.Context, string, []string) error
	Upload(context.Context, Transfer, func(Progress)) error
	Download(context.Context, Transfer, func(Progress)) error
}

type Domain struct{ Name, ID string }
type Record struct {
	ID, Domain, RR, Type, Value, Line string
	TTL, Priority                     int
}

// DNS deliberately exposes no Update/Delete/Disable operations.
type DNS interface {
	ListDomains(context.Context) ([]Domain, error)
	AddDomain(context.Context, string) error
	ListRecords(context.Context, string) ([]Record, error)
	AddRecord(context.Context, Record) (Record, error)
}

// ChallengeDNS is private to the ACME service; callers must verify persisted ownership.
type ChallengeDNS interface {
	DNS
	CreateChallengeRecord(context.Context, Record) (Record, error)
	GetRecord(context.Context, string) (Record, error)
	DeleteChallengeRecord(context.Context, Record) error
}

type Target struct {
	AccountID string `json:"accountId"`
	Kind      string `json:"kind"` // rds, redis, ecs
	ID        string `json:"id"`
	Name      string `json:"name"`
	Region    string `json:"region"`
	Group     string `json:"group"`
	Protocol  string `json:"protocol"`
	Ports     string `json:"ports"`
	Automatic bool   `json:"automatic"`
}
type Whitelist interface {
	ListTargets(context.Context, string, string) ([]Target, error)
	ListRules(context.Context, Target) ([]string, error)
	AddIP(context.Context, Target, string) error
}

// SecurityGroupInspector is an optional read-only capability for detailed rules.
type SecurityGroupInspector interface {
	ListSecurityGroupRules(context.Context, Target) ([]SecurityGroupRule, error)
}

type SecurityGroupRule struct {
	ID, Direction, Policy, Protocol, Ports     string
	Source, Destination, Priority, Description string
}
