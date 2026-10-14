package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"code.gitea.io/sdk/gitea"
	"k8s.io/apimachinery/pkg/types"
	rclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	certv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	hyperv1 "hyperspike.io/gitea-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
)

type Client struct {
	*gitea.Client
	Instance   *hyperv1.Gitea
	httpClient *http.Client
	CA         []byte
	token      string
}

func BuildFromOrg(ctx context.Context, r rclient.Client, instance *hyperv1.OrgRef, ns string) (*Client, *hyperv1.Gitea, error) {
	logger := log.FromContext(ctx)
	if instance == nil {
		return nil, nil, fmt.Errorf("organization reference is required")
	}
	orgName := instance.Name
	orgNamespace := instance.Namespace
	if orgNamespace == "" {
		orgNamespace = ns
	}
	org := &hyperv1.Org{}
	if err := r.Get(ctx, types.NamespacedName{Name: orgName, Namespace: orgNamespace}, org); err != nil {
		logger.Error(err, "failed to get gitea org "+orgName)
		return nil, nil, err
	}
	if org.Spec.Instance.Namespace == "" {
		org.Spec.Instance.Namespace = org.Namespace
	}
	return Build(ctx, r, &org.Spec.Instance, "")
}

func Build(ctx context.Context, r rclient.Client, instance *hyperv1.InstanceType, ns string) (*Client, *hyperv1.Gitea, error) {
	logger := log.FromContext(ctx)

	name := instance.Name
	namespace := instance.Namespace
	if name == "" {
		return nil, nil, fmt.Errorf("instance reference name is required")
	}
	if namespace == "" {
		namespace = ns
	}
	git := &hyperv1.Gitea{}
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, git); err != nil {
		logger.Error(err, "failed to get gitea instance "+name+" in namespace "+namespace)
		return nil, nil, err
	}
	if !git.Status.Ready {
		return nil, nil, nil
	}
	c, err := BuildForInstance(ctx, r, git)
	if err != nil {
		return nil, nil, err
	}
	return c, git, nil
}

// BuildForInstance creates and verifies a Forgejo API client from an already
// fetched instance. Unlike Build it does not require status.ready, so the
// instance reconciler can use it while establishing readiness.
func BuildForInstance(ctx context.Context, r rclient.Client, git *hyperv1.Gitea) (*Client, error) {
	logger := log.FromContext(ctx)
	endpoint, err := Endpoint(git)
	if err != nil {
		return nil, err
	}

	c := &Client{Instance: git}
	if git.Spec.Connection.CABundleSecretRef != nil {
		c.CA, err = secretValue(ctx, r, git.Namespace, git.Spec.Connection.CABundleSecretRef)
		if err != nil {
			return nil, err
		}
	} else if git.Spec.TLS && git.Spec.ManagementPolicy != hyperv1.ManagementPolicyExternal {
		c.CA, err = getCACertificate(ctx, r, git)
		if err != nil {
			logger.Error(err, "failed to get ca certificate")
			return nil, err
		}
	}
	c.httpClient, err = httpClient(ctx, c.CA)
	if err != nil {
		return nil, err
	}

	tokenRef := git.Spec.Connection.AdminTokenSecretRef
	if tokenRef == nil {
		tokenRef = &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: git.Name + "-admin"},
			Key:                  "token",
		}
	}
	token, err := secretValue(ctx, r, git.Namespace, tokenRef)
	if err != nil {
		logger.Error(err, "failed getting admin token secret", "secret", tokenRef.Name)
		return nil, err
	}
	if len(token) == 0 {
		return nil, fmt.Errorf("admin token secret %s/%s key %q is empty", git.Namespace, tokenRef.Name, tokenRef.Key)
	}
	c.token = string(token)

	g, err := gitea.NewClient(endpoint, gitea.SetContext(ctx), gitea.SetToken(c.token), gitea.SetHTTPClient(c.httpClient))
	if err != nil {
		logger.Error(err, "failed to create client for "+endpoint)
		return nil, err
	}
	_, _, err = g.ServerVersion()
	if err != nil {
		logger.Error(err, "failed to get server version "+endpoint)
		return nil, err
	}

	c.Client = g
	return c, nil
}

// Endpoint resolves the configured connection without performing I/O.
func Endpoint(git *hyperv1.Gitea) (string, error) {
	connection := git.Spec.Connection
	if connection.URL != "" && connection.Service != nil {
		return "", fmt.Errorf("connection.url and connection.service are mutually exclusive")
	}
	if connection.URL != "" {
		parsed, err := url.ParseRequestURI(connection.URL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return "", fmt.Errorf("connection.url must be an absolute http(s) URL")
		}
		return connection.URL, nil
	}

	name := git.Name
	namespace := git.Namespace
	scheme := "http"
	port := int32(0)
	if git.Spec.TLS {
		scheme = "https"
	}
	if git.Spec.ManagementPolicy != hyperv1.ManagementPolicyExternal {
		port = git.Spec.Workload.Service.HTTPPort
		if git.Spec.Workload.Service.Name != "" {
			name = git.Spec.Workload.Service.Name
		}
	}
	if connection.Service != nil {
		if connection.Service.Name == "" {
			return "", fmt.Errorf("connection.service.name is required")
		}
		name = connection.Service.Name
		if connection.Service.Namespace != "" {
			namespace = connection.Service.Namespace
		}
		if connection.Service.Scheme != "" {
			scheme = connection.Service.Scheme
		}
		port = connection.Service.Port
	}
	endpoint := scheme + "://" + name + "." + namespace + ".svc"
	if port != 0 && !((scheme == "http" && port == 80) || (scheme == "https" && port == 443)) {
		endpoint += ":" + strconv.Itoa(int(port))
	}
	return endpoint, nil
}

func secretValue(ctx context.Context, r rclient.Client, namespace string, ref *corev1.SecretKeySelector) ([]byte, error) {
	if ref.Name == "" || ref.Key == "" {
		return nil, fmt.Errorf("secret name and key are required")
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, secret); err != nil {
		return nil, err
	}
	value, ok := secret.Data[ref.Key]
	if !ok {
		return nil, fmt.Errorf("secret %s/%s does not contain key %q", namespace, ref.Name, ref.Key)
	}
	return value, nil
}

func httpClient(ctx context.Context, CA []byte) (*http.Client, error) {
	logger := log.FromContext(ctx)
	httpClient := http.Client{
		Timeout: time.Second * 10,
	}
	if CA != nil {
		certpool, _ := x509.SystemCertPool()
		if certpool == nil {
			logger.Info("system cert pool is nil, creating new")
			certpool = x509.NewCertPool()
		}
		if !certpool.AppendCertsFromPEM(CA) {
			return nil, fmt.Errorf("configured CA bundle does not contain a valid PEM certificate")
		}
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    certpool,
				MinVersion: tls.VersionTLS12,
			},
		}
	}
	return &httpClient, nil
}

func getCACertificate(ctx context.Context, r rclient.Client, gitea *hyperv1.Gitea) ([]byte, error) {
	logger := log.FromContext(ctx)

	cert := &certv1.Certificate{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: gitea.Namespace, Name: gitea.Name}, cert); err != nil {
		logger.Error(err, "failed to get ca certificate")
		return []byte{}, err
	}
	if cert.Status.Conditions == nil {
		return []byte{}, nil
	}
	good := false
	for _, cond := range cert.Status.Conditions {
		if cond.Type == certv1.CertificateConditionReady {
			if cond.Status == cmetav1.ConditionTrue {
				good = true
				break
			}
		}
	}
	if !good {
		return []byte{}, nil
	}
	tls := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Namespace: gitea.Namespace, Name: cert.Spec.SecretName}, tls)
	if err != nil {
		logger.Error(err, "failed to get tls secret")
		return []byte{}, err
	}
	return tls.Data["ca.crt"], nil
}

func (c *Client) Get(url string) (resp *http.Response, err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "gitea-operator/0.1.0")
	req.Header.Set("Authorization", c.token)

	return c.httpClient.Do(req)

}
func (c *Client) GetJSON(url string) (resp *http.Response, err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "gitea-operator/0.1.0")
	req.Header.Set("Authorization", "token "+c.token)
	req.Header.Set("Accept", "application/json")

	return c.httpClient.Do(req)

}

func (c *Client) Post(url, contentType string, body io.Reader) (resp *http.Response, err error) {
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, err

	}

	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "gitea-operator/0.1.0")
	req.Header.Set("Authorization", "token "+c.token)

	return c.httpClient.Do(req)

}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", "gitea-operator/0.1.0")
	req.Header.Set("Authorization", "token "+c.token)
	return c.httpClient.Do(req) // #nosec G704

}
