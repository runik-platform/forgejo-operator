/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ManagementPolicy controls whether the operator only connects to an existing
// Forgejo installation or also owns its Kubernetes workload.
// +kubebuilder:validation:Enum=External;Managed
type ManagementPolicy string

const (
	ManagementPolicyExternal ManagementPolicy = "External"
	ManagementPolicyManaged  ManagementPolicy = "Managed"
)

// DeletionPolicy controls what happens to stateful resources when an instance
// is removed. External dependencies are never deleted by this operator.
// +kubebuilder:validation:Enum=Retain;Delete
type DeletionPolicy string

const (
	DeletionPolicyRetain DeletionPolicy = "Retain"
	DeletionPolicyDelete DeletionPolicy = "Delete"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// GiteaSpec defines the desired state of Gitea
type GiteaSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// Selects whether this resource points at an existing Forgejo instance or
	// manages the Forgejo workload. Defaults to Managed for API compatibility.
	// +kubebuilder:default:=Managed
	ManagementPolicy ManagementPolicy `json:"managementPolicy,omitempty"`

	// Connection used by the organization, user, repository and runner
	// controllers. External instances must provide a URL or service reference
	// and an administrator API token Secret.
	Connection ConnectionSpec `json:"connection,omitempty"`

	// Retain is the safe default for stateful resources.
	// +kubebuilder:default:=Retain
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`

	// Ingress for external access
	Ingress IngressSpec `json:"ingress,omitempty"`

	// Object Storage for Gitea
	ObjectStorage *ObjectSpec `json:"objectStorage,omitempty"`

	// Create a loadbalancer for ssh access
	ExternalSSH bool `json:"externalSSH,omitempty"`

	// if different from Hostname
	SSHHostname string `json:"sshHostname,omitempty"`

	// Run the managed Forgejo workload as the rootless image user.
	// +kubebuilder:default:=true
	Rootless bool `json:"rootless,omitempty"`

	// enable prometheus integrations
	// +kubebuilder:default:=false
	Prometheus bool `json:"prometheus,omitempty"`

	// Additional Prometheus Labels
	PrometheusLabels map[string]string `json:"prometheusLabels,omitempty"`

	// Override the operator set image.
	// +kubebuilder:default:="codeberg.org/forgejo/forgejo:16.0.2-rootless"
	Image string `json:"image,omitempty"`

	// Use Valkey
	// +kubebuilder:default:=false
	Valkey bool `json:"valkey,omitempty"`

	// Use TLS
	// +kubebuilder:default:=false
	TLS bool `json:"tls,omitempty"`

	// TLS Cert-manager Issuer
	CertIssuer string `json:"certIssuer,omitempty"`

	// Cert-Manger Cluster Issuer Kind
	// +kubebuilder:default:="ClusterIssuer"
	// +kubebuilder:validation:Enum=ClusterIssuer;Issuer
	CertIssuerType string `json:"certIssuerType,omitempty"`

	ClusterDomain string `json:"clusterDomain,omitempty"`

	// Postgres Configuration
	Postgres PostgresSpec `json:"postgres,omitempty"`

	// Persistence used by a managed Forgejo workload.
	Persistence PersistenceSpec `json:"persistence,omitempty"`

	// Workload customizes the managed StatefulSet without embedding secret
	// values in the custom resource.
	Workload WorkloadSpec `json:"workload,omitempty"`
}

type ConnectionSpec struct {
	// URL is an explicit Forgejo base URL. It is mutually exclusive with Service.
	// +kubebuilder:validation:Pattern=`^https?://`
	URL string `json:"url,omitempty"`

	// Service resolves an in-cluster Forgejo endpoint.
	Service *ServiceEndpoint `json:"service,omitempty"`

	// AdminTokenSecretRef contains the Forgejo administrator API token.
	AdminTokenSecretRef *corev1.SecretKeySelector `json:"adminTokenSecretRef,omitempty"`

	// CABundleSecretRef optionally contains a PEM CA bundle for HTTPS endpoints.
	CABundleSecretRef *corev1.SecretKeySelector `json:"caBundleSecretRef,omitempty"`
}

type ServiceEndpoint struct {
	Name string `json:"name"`

	// Namespace defaults to the Instance namespace.
	Namespace string `json:"namespace,omitempty"`

	// +kubebuilder:validation:Enum=http;https
	Scheme string `json:"scheme,omitempty"`

	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`
}

type PersistenceSpec struct {
	Data   VolumeSpec  `json:"data,omitempty"`
	Config *VolumeSpec `json:"config,omitempty"`
}

type VolumeSpec struct {
	// ExistingClaim reuses a PVC without taking ownership of it.
	ExistingClaim string `json:"existingClaim,omitempty"`
	MountPath     string `json:"mountPath,omitempty"`
	Size          string `json:"size,omitempty"`
	StorageClass  string `json:"storageClass,omitempty"`
}

type WorkloadSpec struct {
	// Name overrides the managed StatefulSet name. This permits a parallel
	// cutover from an existing deployment without renaming the Instance.
	Name string `json:"name,omitempty"`

	// +kubebuilder:validation:Minimum=0
	Replicas *int32 `json:"replicas,omitempty"`

	// +kubebuilder:default:=false
	SSHEnabled *bool `json:"sshEnabled,omitempty"`

	// BootstrapAdmin creates and maintains the legacy local administrator. Set
	// it to false when adopting an instance that already has an API token.
	// +kubebuilder:default:=true
	BootstrapAdmin *bool `json:"bootstrapAdmin,omitempty"`

	// SpoonEnabled enables the optional legacy gitea-spoon sidecar.
	// +kubebuilder:default:=false
	SpoonEnabled *bool `json:"spoonEnabled,omitempty"`

	// SpoonImage overrides the legacy sidecar image when it is enabled.
	SpoonImage string `json:"spoonImage,omitempty"`

	// RepositoryRoot defaults to /data/git/gitea-repositories for compatibility.
	RepositoryRoot string `json:"repositoryRoot,omitempty"`

	// AppINIPath defaults to /data/gitea/conf/app.ini for compatibility.
	AppINIPath string `json:"appIniPath,omitempty"`

	Service ManagedServiceSpec `json:"service,omitempty"`

	// Env adds or overrides Forgejo environment variables. Secret values should
	// use valueFrom.secretKeyRef; they are also available to Forgejo init steps.
	Env []corev1.EnvVar `json:"env,omitempty"`

	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	Affinity    *corev1.Affinity    `json:"affinity,omitempty"`
}

type ManagedServiceSpec struct {
	// Name defaults to the Instance name.
	Name string `json:"name,omitempty"`

	// Managed controls whether the operator owns the Service.
	// +kubebuilder:default:=true
	Managed *bool `json:"managed,omitempty"`

	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	HTTPPort int32 `json:"httpPort,omitempty"`

	// +kubebuilder:default:=2222
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	SSHPort int32 `json:"sshPort,omitempty"`
}

type PostgresSpec struct {
	// Postgres Provider
	// +kubebuilder:default:="zalando"
	// +kubebuilder:validation:Enum=zalando;cnpg
	Provider string `json:"provider,omitempty"`

	// Existing connects a managed Forgejo workload to a database provisioned by
	// the platform. When set, the operator never creates or owns a Postgres CR.
	Existing *ExistingPostgresSpec `json:"existing,omitempty"`
}

type ExistingPostgresSpec struct {
	Host string `json:"host"`
	Port int32  `json:"port,omitempty"`

	// Database or DatabaseSecretRef must resolve the existing database name.
	Database          string                    `json:"database,omitempty"`
	DatabaseSecretRef *corev1.SecretKeySelector `json:"databaseSecretRef,omitempty"`
	SSLMode           string                    `json:"sslMode,omitempty"`

	CredentialsSecretRef corev1.LocalObjectReference `json:"credentialsSecretRef"`
	UsernameKey          string                      `json:"usernameKey,omitempty"`
	PasswordKey          string                      `json:"passwordKey,omitempty"`
}

type ObjectSpec struct {
	// Object Storage Type
	// +kubebuilder:default:="minio"
	// +kubebuilder:validation:Enum=minio;gcs;s3
	Type string `json:"type,omitempty"`

	// Object Storage Endpoint
	Endpoint string `json:"endpoint,omitempty"`

	// Object Cloud Provider Region
	Region string `json:"region,omitempty"`

	// Existing consumes an existing bucket and credentials. The operator never
	// creates or deletes either resource when this field is set.
	Existing *ExistingObjectStorageSpec `json:"existing,omitempty"`
}

type ExistingObjectStorageSpec struct {
	Bucket string `json:"bucket,omitempty"`

	BucketSecretRef *corev1.SecretKeySelector `json:"bucketSecretRef,omitempty"`

	CredentialsSecretRef corev1.LocalObjectReference `json:"credentialsSecretRef"`
	AccessKey            string                      `json:"accessKey,omitempty"`
	SecretKey            string                      `json:"secretKey,omitempty"`

	// +kubebuilder:default:=true
	Secure *bool `json:"secure,omitempty"`
}

type IngressSpec struct {
	// Managed controls whether the operator owns an Ingress.
	// +kubebuilder:default:=true
	Managed *bool `json:"managed,omitempty"`

	// External Hostname of the instance
	// +kubebuilder:example:=git.local
	Host string `json:"host,omitempty"`

	// Ingress Annotations
	Annotations map[string]string `json:"annotations,omitempty"`
}

// GiteaStatus defines the observed state of Gitea
type GiteaStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Conditions         []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
	Ready              bool               `json:"ready"`
	Endpoint           string             `json:"endpoint,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Gitea is the Schema for the gitea API
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Policy",type="string",JSONPath=`.spec.managementPolicy`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Hostname",type="string",priority=1,JSONPath=".spec.hostname"
// +kubebuilder:printcolumn:name="Rootless",type="boolean",priority=1,JSONPath=".spec.rootless"
// +kubebuilder:printcolumn:name="Image",type="string",priority=1,JSONPath=".spec.image"
type Gitea struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GiteaSpec   `json:"spec,omitempty"`
	Status GiteaStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GiteaList contains a list of Gitea
type GiteaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Gitea `json:"items"`
}
