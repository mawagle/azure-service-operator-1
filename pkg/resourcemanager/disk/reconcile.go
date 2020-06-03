// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package disk

import (
	"context"
	"fmt"
	"strings"

	azurev1alpha1 "github.com/Azure/azure-service-operator/api/v1alpha1"
	"github.com/Azure/azure-service-operator/pkg/errhelp"
	"github.com/Azure/azure-service-operator/pkg/helpers"
	"github.com/Azure/azure-service-operator/pkg/resourcemanager"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func (g *AzureDiskClient) Ensure(ctx context.Context, obj runtime.Object, opts ...resourcemanager.ConfigOption) (bool, error) {

	instance, err := g.convert(obj)
	if err != nil {
		return true, err
	}

	client := getDiskClient()

	location := instance.Spec.Location
	resourceGroup := instance.Spec.ResourceGroup
	resourceName := instance.Name
	createOption := instance.Spec.CreateOption
	diskSizeGB := instance.Spec.DiskSizeGB

	instance.Status.Provisioning = true

	// Check if this item already exists. This is required
	// to overcome the issue with the lack of idempotence of the Create call
	item, err := g.GetDisk(ctx, resourceGroup, resourceName)
	if err == nil {
		instance.Status.Provisioned = true
		instance.Status.Provisioning = false
		instance.Status.Message = resourcemanager.SuccessMsg
		instance.Status.ResourceId = *item.ID
		return true, nil
	}

	// TODO: Update this validation as support for new CreateOption types is added.
	// Validate CreateOption. Currently only 'Empty' is supported.
	if !strings.EqualFold(createOption, "Empty") {
		instance.Status.Provisioned = false
		instance.Status.Provisioning = false
		instance.Status.Message = errhelp.UnsupportedDiskCreateOption
		return false, nil
	}

	future, err := g.CreateDisk(
		ctx,
		location,
		resourceGroup,
		resourceName,
		createOption,
		diskSizeGB,
	)
	if err != nil {
		// let the user know what happened
		instance.Status.Message = err.Error()
		instance.Status.Provisioning = false
		// errors we expect might happen that we are ok with waiting for
		catch := []string{
			errhelp.ResourceGroupNotFoundErrorCode,
			errhelp.ParentNotFoundErrorCode,
			errhelp.NotFoundErrorCode,
			errhelp.AsyncOpIncompleteError,
			errhelp.ResourceNotFound,
			errhelp.InvalidResourceReference,
		}

		azerr := errhelp.NewAzureErrorAzureError(err)
		if helpers.ContainsString(catch, azerr.Type) {
			// most of these error technically mean the resource is actually not provisioning
			switch azerr.Type {
			case errhelp.AsyncOpIncompleteError:
				instance.Status.Provisioning = true
			}
			// reconciliation is not done but error is acceptable
			return false, nil
		}
		// reconciliation not done and we don't know what happened
		return false, err
	}

	_, err = future.Result(client)
	if err != nil {
		// let the user know what happened
		instance.Status.Message = err.Error()
		instance.Status.Provisioning = false
		// errors we expect might happen that we are ok with waiting for
		catch := []string{
			errhelp.ResourceGroupNotFoundErrorCode,
			errhelp.ParentNotFoundErrorCode,
			errhelp.NotFoundErrorCode,
			errhelp.AsyncOpIncompleteError,
			errhelp.SubscriptionDoesNotHaveServer,
		}

		azerr := errhelp.NewAzureErrorAzureError(err)
		if helpers.ContainsString(catch, azerr.Type) {
			// most of these error technically mean the resource is actually not provisioning
			switch azerr.Type {
			case errhelp.AsyncOpIncompleteError:
				instance.Status.Provisioning = true
			}
			// reconciliation is not done but error is acceptable
			return false, nil
		}
		// reconciliation not done and we don't know what happened
		return false, err
	}

	if instance.Status.Provisioning {
		instance.Status.Provisioned = true
		instance.Status.Provisioning = false
		instance.Status.Message = resourcemanager.SuccessMsg
	} else {
		instance.Status.Provisioned = false
		instance.Status.Provisioning = true
	}
	return true, nil
}

func (g *AzureDiskClient) Delete(ctx context.Context, obj runtime.Object, opts ...resourcemanager.ConfigOption) (bool, error) {

	instance, err := g.convert(obj)
	if err != nil {
		return true, err
	}

	resourceGroup := instance.Spec.ResourceGroup
	resourceName := instance.Name

	status, err := g.DeleteDisk(
		ctx,
		resourceName,
		resourceGroup,
	)
	if err != nil {
		catch := []string{
			errhelp.AsyncOpIncompleteError,
		}
		gone := []string{
			errhelp.ResourceGroupNotFoundErrorCode,
			errhelp.ParentNotFoundErrorCode,
			errhelp.NotFoundErrorCode,
			errhelp.ResourceNotFound,
		}
		azerr := errhelp.NewAzureErrorAzureError(err)
		if helpers.ContainsString(catch, azerr.Type) {
			return true, nil
		} else if helpers.ContainsString(gone, azerr.Type) {
			return false, nil
		}
		return true, err
	}

	if err == nil {
		if status != "InProgress" {
			return false, nil
		}
	}

	return true, nil
}

func (g *AzureDiskClient) GetParents(obj runtime.Object) ([]resourcemanager.KubeParent, error) {

	instance, err := g.convert(obj)
	if err != nil {
		return nil, err
	}

	return []resourcemanager.KubeParent{
		{
			Key: types.NamespacedName{
				Namespace: instance.Namespace,
				Name:      instance.Spec.ResourceGroup,
			},
			Target: &azurev1alpha1.ResourceGroup{},
		},
	}, nil
}

func (g *AzureDiskClient) GetStatus(obj runtime.Object) (*azurev1alpha1.ASOStatus, error) {

	instance, err := g.convert(obj)
	if err != nil {
		return nil, err
	}
	return &instance.Status, nil
}

func (g *AzureDiskClient) convert(obj runtime.Object) (*azurev1alpha1.AzureDisk, error) {
	local, ok := obj.(*azurev1alpha1.AzureDisk)
	if !ok {
		return nil, fmt.Errorf("failed type assertion on kind: %s", obj.GetObjectKind().GroupVersionKind().String())
	}
	return local, nil
}