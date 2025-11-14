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

package oras

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	utils "github.com/NVIDIA/k8s-kata-manager/internal/utils"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"k8s.io/klog/v2"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// Artifact struc holds the information about the oras artifact
type Artifact struct {
	Registry   string
	Repository string
	Tag        string

	Output string
}

// NewArtifact returns a new instance of Artifact
func NewArtifact(ref string, output string) (*Artifact, error) {
	var repository, tag string

	registry, err := utils.ParseRegistry(ref)
	if err != nil {
		return nil, err
	}

	if idx := strings.LastIndex(ref, "@"); idx != -1 {
		repository = ref[:idx]
		tag = ref[idx+1:]
	} else if idx := strings.LastIndex(ref, ":"); idx != 1 {
		repository = ref[:idx]
		tag = ref[idx+1:]
	} else {
		return nil, fmt.Errorf("unable to parse tag or digest")
	}

	return &Artifact{
		Registry:   registry,
		Repository: repository,
		Tag:        tag,
		Output:     output,
	}, nil
}

// Pull pulls the artifact from the remote repository into a local path
func (a *Artifact) Pull(ctx context.Context, creds *auth.Credential) (ocispec.Descriptor, error) {
	// Connect to a remote repository
	repo, err := remote.NewRepository(a.Repository)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	if creds != nil {
		repo.Client = &auth.Client{
			Client: retry.DefaultClient,
			Cache:  auth.DefaultCache,
			Credential: auth.StaticCredential(a.Registry, auth.Credential{
				Username: creds.Username,
				Password: creds.Password,
			}),
		}
	}

	// Try to copy with platform-specific options first
	copyOpts := oras.DefaultCopyOptions
	copyOpts.WithTargetPlatform(&ocispec.Platform{
		Architecture: runtime.GOARCH,
		OS:           runtime.GOOS,
	})

	// Create a file store
	fs, err := file.New(a.Output)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	defer fs.Close()

	desc, err := oras.Copy(ctx, repo, a.Tag, fs, a.Tag, copyOpts)
	if err != nil {
		klog.Warningf("Platform-specific copy failed for %s (platform: %s/%s): %v", a.Repository, runtime.GOOS, runtime.GOARCH, err)
		klog.Infof("Falling back to default copy options without platform specification")

		// Close the first file store before creating a new one
		fs.Close()

		// If platform-specific copy fails, fall back to default options with a fresh file store
		// This handles cases where the manifest doesn't specify a platform
		fs, err = file.New(a.Output)
		if err != nil {
			klog.Errorf("Failed to create file store for fallback copy: %v", err)
			return ocispec.Descriptor{}, err
		}
		defer fs.Close()

		desc, err := oras.Copy(ctx, repo, a.Tag, fs, a.Tag, oras.DefaultCopyOptions)
		if err != nil {
			klog.Errorf("Fallback copy also failed for %s: %v", a.Repository, err)
			return ocispec.Descriptor{}, err
		}

		klog.Infof("Successfully pulled artifact using fallback method")
		return desc, nil
	}

	klog.V(2).Infof("Successfully pulled artifact %s with platform-specific options", a.Repository)
	return desc, nil
}
