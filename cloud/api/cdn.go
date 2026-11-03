package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// The public files (the public pages, the shares, and the pictures of the comments on pull
// requests) have a long max-age at the edge of CloudFront. When the service deletes one, the edge
// can serve the old copy for up to a year. So a delete of a public file also removes it from the
// edge: CDNStore notes the key, and the server sends one invalidation at the end of the request.

// publicPrefixes are the folders of the bucket that CloudFront serves to everyone, at the same
// path.
var publicPrefixes = []string{"p/", "s/", "gh/"}

// Invalidator removes paths from the edge of the CDN.
type Invalidator interface {
	Invalidate(ctx context.Context, paths []string) error
}

// CDNStore is a Store that notes the deletes of public files for the CDN.
type CDNStore struct {
	Store
	mu   sync.Mutex
	gone map[string]bool
}

// Delete deletes the file at key, and notes it when it is public.
func (c *CDNStore) Delete(ctx context.Context, key string) error {
	err := c.Store.Delete(ctx, key)
	if err == nil && isPublicKey(key) {
		c.mu.Lock()
		if c.gone == nil {
			c.gone = map[string]bool{}
		}
		c.gone[key] = true
		c.mu.Unlock()
	}
	return err
}

// Calls passes on the count of the calls of the bucket.
func (c *CDNStore) Calls() int64 { return storeCalls(c.Store) }

// Gone returns the public keys that were deleted since the last call, and forgets them.
func (c *CDNStore) Gone() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]string, 0, len(c.gone))
	for k := range c.gone {
		keys = append(keys, k)
	}
	c.gone = nil
	sort.Strings(keys)
	return keys
}

func isPublicKey(key string) bool {
	for _, p := range publicPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// invalidPaths makes the paths of an invalidation from the deleted keys. More than 5 keys in one
// folder of a page or a share ("p/<slug>/", "s/<id>/") become one path with "*": an invalidation
// pays for each path.
func invalidPaths(keys []string) []string {
	groups := map[string][]string{}
	for _, k := range keys {
		g := k
		if parts := strings.SplitN(k, "/", 3); len(parts) == 3 && (parts[0] == "p" || parts[0] == "s") {
			g = parts[0] + "/" + parts[1] + "/"
		}
		groups[g] = append(groups[g], k)
	}
	var out []string
	for g, ks := range groups {
		if strings.HasSuffix(g, "/") && len(ks) > 5 {
			out = append(out, "/"+g+"*")
			continue
		}
		for _, k := range ks {
			out = append(out, "/"+k)
		}
	}
	sort.Strings(out)
	return out
}

// flushCDN sends the invalidation of the public files that the request deleted.
func (s *Server) flushCDN(ctx context.Context) {
	cs, ok := s.Store.(*CDNStore)
	if !ok || s.CDN == nil {
		return
	}
	keys := cs.Gone()
	if len(keys) == 0 {
		return
	}
	if err := s.CDN.Invalidate(ctx, invalidPaths(keys)); err != nil {
		log.Printf("api: the invalidation of %d files: %v", len(keys), err)
	}
}

// CloudFront invalidates paths of the distribution of Domain. It finds the distribution by its
// alias once: the stack cannot give the ID of the distribution to the function, because the
// distribution names the URL of the function.
type CloudFront struct {
	Client *cloudfront.Client
	Domain string
	mu     sync.Mutex
	id     string
}

func (c *CloudFront) distribution(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.id != "" {
		return c.id, nil
	}
	p := cloudfront.NewListDistributionsPaginator(c.Client, &cloudfront.ListDistributionsInput{})
	for p.HasMorePages() {
		out, err := p.NextPage(ctx)
		if err != nil {
			return "", err
		}
		if out.DistributionList == nil {
			break
		}
		for _, d := range out.DistributionList.Items {
			if d.Aliases == nil {
				continue
			}
			for _, a := range d.Aliases.Items {
				if strings.EqualFold(a, c.Domain) {
					c.id = aws.ToString(d.Id)
					return c.id, nil
				}
			}
		}
	}
	return "", fmt.Errorf("no CloudFront distribution has the alias %s", c.Domain)
}

// Invalidate removes the paths from the edge.
func (c *CloudFront) Invalidate(ctx context.Context, paths []string) error {
	id, err := c.distribution(ctx)
	if err != nil {
		return err
	}
	b := make([]byte, 6)
	rand.Read(b)
	ref := time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b)
	_, err = c.Client.CreateInvalidation(ctx, &cloudfront.CreateInvalidationInput{
		DistributionId: &id,
		InvalidationBatch: &cftypes.InvalidationBatch{
			CallerReference: &ref,
			Paths:           &cftypes.Paths{Quantity: aws.Int32(int32(len(paths))), Items: paths},
		},
	})
	return err
}
