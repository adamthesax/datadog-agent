// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-2020 Datadog, Inc.

// +build kubeapiserver

package common

import (
	"io/ioutil"

	"github.com/google/uuid"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/DataDog/datadog-agent/pkg/config"
	"github.com/DataDog/datadog-agent/pkg/util/log"
)

const (
	defaultClusterIDMap = "datadog-cluster-id"
)

// GetResourcesNamespace is used to fetch the namespace of the resources used by the Kubernetes check (e.g. Leader Election, Event collection).
func GetResourcesNamespace() string {
	namespace := config.Datadog.GetString("kube_resources_namespace")
	if namespace != "" {
		return namespace
	}
	log.Debugf("No configured namespace for the resource, fetching from the current context")
	return GetMyNamespace()
}

// GetMyNamespace returns the namespace our pod is running in
func GetMyNamespace() string {
	namespacePath := "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	val, e := ioutil.ReadFile(namespacePath)
	if e == nil && val != nil {
		return string(val)
	}
	log.Warnf("There was an error fetching the namespace from the context, using default")
	return "default"
}

// GetOrCreateClusterID generates a cluster ID and persists it to a ConfigMap.
// It first checks if the CM exists, in which case it uses the ID it contains
// It thus requires get, create, and update perms on configmaps in the cluster-agent's namespace
func GetOrCreateClusterID(coreClient corev1.CoreV1Interface) (string, error) {
	myNS := GetMyNamespace()

	cm, err := coreClient.ConfigMaps(myNS).Get(defaultClusterIDMap, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) == false {
			log.Errorf("Cannot retrieve ConfigMap %s/%s: %s", myNS, defaultClusterIDMap, err)
			return "", err
		}
		// the config map doesn't exist yet, generate a UUID and persist it
		clusterID := uuid.New().String()
		cm := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      defaultClusterIDMap,
				Namespace: myNS,
			},
			Data: map[string]string{
				"id": clusterID,
			},
		}
		cm, err = coreClient.ConfigMaps(myNS).Create(cm)
		if err != nil {
			log.Errorf("Cannot create ConfigMap %s/%s: %s", myNS, defaultClusterIDMap, err)
			return "", err
		}
		return clusterID, nil
	}

	// config map exists, use its content or update it if the content doesn't look right
	clusterID, found := cm.Data["id"]
	if found == true && len([]byte(clusterID)) == 36 {
		return clusterID, nil
	}

	log.Warnf("Content of ConfigMap %s/%s doesn't look like a cluster ID, updating it", myNS, defaultClusterIDMap)
	clusterID = uuid.New().String()
	cm.Data["id"] = clusterID
	_, err = coreClient.ConfigMaps(myNS).Update(cm)
	if err != nil {
		log.Errorf("Failed to update ConfigMap %s/%s with correct cluster ID: %s", myNS, defaultClusterIDMap, err)
		return "", err
	}
	return clusterID, nil
}
