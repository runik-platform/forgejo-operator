package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	hyperv1 "hyperspike.io/gitea-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		instance *hyperv1.Gitea
		want     string
		wantErr  bool
	}{
		{
			name: "managed default service",
			instance: &hyperv1.Gitea{ObjectMeta: metav1.ObjectMeta{
				Name: "forgejo", Namespace: "forgejo",
			}},
			want: "http://forgejo.forgejo.svc",
		},
		{
			name: "external service",
			instance: &hyperv1.Gitea{
				ObjectMeta: metav1.ObjectMeta{Name: "instance", Namespace: "control"},
				Spec: hyperv1.GiteaSpec{
					ManagementPolicy: hyperv1.ManagementPolicyExternal,
					Connection: hyperv1.ConnectionSpec{Service: &hyperv1.ServiceEndpoint{
						Name: "forgejo", Namespace: "apps", Port: 3000,
					}},
				},
			},
			want: "http://forgejo.apps.svc:3000",
		},
		{
			name: "explicit URL",
			instance: &hyperv1.Gitea{Spec: hyperv1.GiteaSpec{
				Connection: hyperv1.ConnectionSpec{URL: "https://git.example.test"},
			}},
			want: "https://git.example.test",
		},
		{
			name: "conflicting endpoint sources",
			instance: &hyperv1.Gitea{Spec: hyperv1.GiteaSpec{Connection: hyperv1.ConnectionSpec{
				URL:     "https://git.example.test",
				Service: &hyperv1.ServiceEndpoint{Name: "forgejo"},
			}}},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Endpoint(test.instance)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Endpoint() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("Endpoint() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBuildForExternalInstanceUsesReferencedToken(t *testing.T) {
	const token = "test-admin-token"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/version" {
			http.NotFound(response, request)
			return
		}
		if request.Header.Get("Authorization") != "token "+token {
			http.Error(response, "missing token", http.StatusUnauthorized)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"version":"16.0.2"}`))
	}))
	defer server.Close()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "forgejo-api", Namespace: "forgejo"},
		Data:       map[string][]byte{"token": []byte(token)},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	instance := &hyperv1.Gitea{
		ObjectMeta: metav1.ObjectMeta{Name: "forgejo", Namespace: "forgejo"},
		Spec: hyperv1.GiteaSpec{
			ManagementPolicy: hyperv1.ManagementPolicyExternal,
			Connection: hyperv1.ConnectionSpec{
				URL: server.URL,
				AdminTokenSecretRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name},
					Key:                  "token",
				},
			},
		},
	}

	if _, err := BuildForInstance(context.Background(), kubeClient, instance); err != nil {
		t.Fatalf("BuildForInstance() error = %v", err)
	}
}
