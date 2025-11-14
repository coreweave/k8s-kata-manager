/*
 * Copyright (c), NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

var nodeName string

// NodeLabelAction defines the action to perform on node labels
type NodeLabelAction string

const (
	// NodeLabelActionAdd adds or updates labels
	NodeLabelActionAdd NodeLabelAction = "add"
	// NodeLabelActionRemove removes labels
	NodeLabelActionRemove NodeLabelAction = "remove"
)

// K8sCli is a Kubernetes client with Secret and Node interfaces
type K8sCli struct {
	corev1.SecretInterface
	corev1.NodeInterface

	namespace string
}

func NewClient(namespace string) K8sCli {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	k := K8sCli{
		clientset.CoreV1().Secrets(namespace),
		clientset.CoreV1().Nodes(),
		namespace}
	return k
}

// NodeName returns the name of the k8s node we're running on.
func NodeName() string {
	if nodeName == "" {
		nodeName = os.Getenv("NODE_NAME")
	}
	return nodeName
}

// GetKubernetesNamespace returns the kubernetes namespace we're running under,
// or an empty string if the namespace cannot be determined.
func GetKubernetesNamespace() string {
	const kubernetesNamespaceFilePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	if _, err := os.Stat(kubernetesNamespaceFilePath); err == nil {
		data, err := os.ReadFile(kubernetesNamespaceFilePath)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return os.Getenv("KUBERNETES_NAMESPACE")
}

// LabelNode adds, updates, or removes labels on the current node based on the action.
// For NodeLabelActionAdd: labels is a map of label keys to label values to add or update.
// For NodeLabelActionRemove: labels is a map of label keys to remove (values are ignored).
func (k *K8sCli) LabelNode(ctx context.Context, action NodeLabelAction, labels map[string]string) error {
	nodeName := NodeName()
	if nodeName == "" {
		return fmt.Errorf("node name is not set")
	}

	// Validate action
	if action != NodeLabelActionAdd && action != NodeLabelActionRemove {
		return fmt.Errorf("invalid action %q: must be %q or %q", action, NodeLabelActionAdd, NodeLabelActionRemove)
	}

	// Build the patch payload
	patchLabels := make(map[string]*string)
	switch action {
	case NodeLabelActionAdd:
		// For adding labels, set the string value
		for key, value := range labels {
			v := value
			patchLabels[key] = &v
		}
	case NodeLabelActionRemove:
		// For removing labels, set to nil (null in JSON)
		for key := range labels {
			patchLabels[key] = nil
		}
	}

	// Create the strategic merge patch
	patch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": patchLabels,
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %w", err)
	}

	// Apply the patch
	_, err = k.NodeInterface.Patch(ctx, nodeName, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to patch node %s: %w", nodeName, err)
	}

	return nil
}
