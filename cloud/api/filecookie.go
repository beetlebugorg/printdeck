package api

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The files of the projects behind CloudFront with signed cookies. A link to a file of a project
// is its path on the domain, /x/<project>/..., and each reply of the API with such links sets the
// signed cookies of the project: CloudFront checks them at each request of a file, and keeps the
// files in its cache. The cookies of a project have the path /x/<project>/, so a person gets the
// files of the projects that the API shows them, and of no other project.

// FileSigner signs the cookies of the files of the projects (CloudFront signed cookies with a
// custom policy).
type FileSigner struct {
	Domain string
	KeyID  string // the ID of the public key in CloudFront
	Key    *rsa.PrivateKey
}

// ParseFileKey reads the private key of the signer: PEM, PKCS#1 or PKCS#8.
func ParseFileKey(pemText string) (*rsa.PrivateKey, error) {
	b, _ := pem.Decode([]byte(pemText))
	if b == nil {
		return nil, errors.New("the key of the files is no PEM")
	}
	if k, err := x509.ParsePKCS1PrivateKey(b.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(b.Bytes)
	if err != nil {
		return nil, err
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("the key of the files is no RSA key")
	}
	return rk, nil
}

// cfBase64 is base64 with the characters that CloudFront wants in a cookie.
func cfBase64(b []byte) string {
	return strings.NewReplacer("+", "-", "=", "_", "/", "~").Replace(base64.StdEncoding.EncodeToString(b))
}

// Cookies returns the signed cookies of the files of the project, until exp.
func (f *FileSigner) Cookies(project string, exp time.Time) ([]*http.Cookie, error) {
	policy := fmt.Sprintf(`{"Statement":[{"Resource":"https://%s/x/%s/*","Condition":{"DateLessThan":{"AWS:EpochTime":%d}}}]}`,
		f.Domain, project, exp.Unix())
	sum := sha1.Sum([]byte(policy))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.Key, crypto.SHA1, sum[:])
	if err != nil {
		return nil, err
	}
	path := "/x/" + project + "/"
	mk := func(name, value string) *http.Cookie {
		return &http.Cookie{Name: name, Value: value, Path: path, Expires: exp, Secure: true, HttpOnly: true,
			SameSite: http.SameSiteLaxMode}
	}
	return []*http.Cookie{mk("CloudFront-Policy", cfBase64([]byte(policy))), mk("CloudFront-Signature", cfBase64(sig)),
		mk("CloudFront-Key-Pair-Id", f.KeyID)}, nil
}

// fileJar keeps the projects whose files the reply of a request links to.
type fileJar struct {
	mu       sync.Mutex
	projects map[string]bool
}

type fileJarKey struct{}

// withFileJar returns ctx with an empty jar.
func withFileJar(ctx context.Context) (context.Context, *fileJar) {
	j := &fileJar{projects: map[string]bool{}}
	return context.WithValue(ctx, fileJarKey{}, j), j
}

// noteFile records that the reply of the request of ctx links to the file at key of a project.
func noteFile(ctx context.Context, key string) {
	j, _ := ctx.Value(fileJarKey{}).(*fileJar)
	rest, ok := strings.CutPrefix(key, "x/")
	if j == nil || !ok {
		return
	}
	project, _, _ := strings.Cut(rest, "/")
	if !validID(project) {
		return
	}
	j.mu.Lock()
	j.projects[project] = true
	j.mu.Unlock()
}

// setFileCookies sets the signed cookies of the projects of the jar on the reply.
func (s *Server) setFileCookies(w http.ResponseWriter, j *fileJar) {
	if s.Files == nil || j == nil {
		return
	}
	exp := s.now().Add(urlTTL)
	j.mu.Lock()
	defer j.mu.Unlock()
	for p := range j.projects {
		cs, err := s.Files.Cookies(p, exp)
		if err != nil {
			continue
		}
		for _, c := range cs {
			http.SetCookie(w, c)
		}
	}
}
