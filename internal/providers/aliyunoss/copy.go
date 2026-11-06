package aliyunoss

import (
	"context"
	"errors"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
)

// conditionalCopyClient closes an SDK v1.6.1 Copier gap: its multipart path
// constructs UploadPartCopyRequest without copying source preconditions. Keep
// the SDK's metadata, tagging, ranges, completion and abort machinery, but bind
// every part to the immutable identity observed before starting the copy.
type conditionalCopyClient struct {
	*oss.Client
	etag, versionID string
}

var _ oss.CopyAPIClient = conditionalCopyClient{}

func (c conditionalCopyClient) UploadPartCopy(ctx context.Context, request *oss.UploadPartCopyRequest, options ...func(*oss.Options)) (*oss.UploadPartCopyResult, error) {
	if request == nil || c.etag == "" {
		return nil, errors.New("multipart copy requires a pinned source identity")
	}
	guarded := *request
	guarded.IfMatch = oss.Ptr(c.etag)
	guarded.SourceVersionId = optional(c.versionID)
	return c.Client.UploadPartCopy(ctx, &guarded, options...)
}

func (c *Client) copyPinned(ctx context.Context, request *oss.CopyObjectRequest) error {
	guarded := conditionalCopyClient{Client: c.sdk, etag: value(request.IfMatch), versionID: value(request.SourceVersionId)}
	_, err := oss.NewCopier(guarded).Copy(ctx, request)
	return err
}
