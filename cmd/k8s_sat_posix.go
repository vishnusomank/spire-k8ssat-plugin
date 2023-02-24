package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang-jwt/jwt"
	"github.com/hashicorp/go-hclog"
	"github.com/rs/zerolog/log"
	review "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var parsed bool = false
var kubeconfig *string

func isInCluster() bool {
	if _, ok := os.LookupEnv("KUBERNETES_PORT"); ok {
		return true
	}

	return false
}

func ConnectK8sClient() *kubernetes.Clientset {
	if isInCluster() {
		return ConnectInClusterAPIClient()
	}

	return ConnectLocalAPIClient()
}

func ConnectLocalAPIClient() *kubernetes.Clientset {
	if !parsed {
		homeDir := ""
		if h := os.Getenv("HOME"); h != "" {
			homeDir = h
		} else {
			homeDir = os.Getenv("USERPROFILE") // windows
		}

		envKubeConfig := os.Getenv("KUBECONFIG")
		if envKubeConfig != "" {
			kubeconfig = &envKubeConfig
		} else {
			if home := homeDir; home != "" {
				kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
			} else {
				kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
			}
			flag.Parse()
		}

		parsed = true
	}

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Error().Msg(err.Error())
		return nil
	}

	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Error().Msg(err.Error())
		return nil
	}

	return clientset
}

func ConnectInClusterAPIClient() *kubernetes.Clientset {
	host := ""
	port := ""
	token := ""

	if val, ok := os.LookupEnv("KUBERNETES_SERVICE_HOST"); ok {
		host = val
	} else {
		host = "127.0.0.1"
	}

	if val, ok := os.LookupEnv("KUBERNETES_PORT_443_TCP_PORT"); ok {
		port = val
	} else {
		port = "6443"
	}

	read, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		log.Error().Msg(err.Error())
		return nil
	}

	token = string(read)

	// create the configuration by token
	kubeConfig := &rest.Config{
		Host:        "https://" + host + ":" + port,
		BearerToken: token,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
	}

	if client, err := kubernetes.NewForConfig(kubeConfig); err != nil {
		log.Error().Msg(err.Error())
		return nil
	} else {
		return client
	}
}

func (p *Plugin) getSelectorValuesFromPodName(podName, podNs string, log hclog.Logger) ([]string, error) {

	if podName == "" {
		log.Warn("pod name is empty")
		return nil, fmt.Errorf("pod name is empty")
	}
	var selectorValues []string

	pod, err := p.client.CoreV1().Pods(podNs).Get(context.Background(), podName, metav1.GetOptions{})
	if err != nil {
		log.Warn("get pod error ", err)
		return nil, err
	}

	if pod.Name == podName {
		selectorValues = []string{
			fmt.Sprintf("sa:%s", pod.Spec.ServiceAccountName),
			fmt.Sprintf("ns:%s", pod.Namespace),
			fmt.Sprintf("pod-uid:%s", pod.UID),
			fmt.Sprintf("pod-name:%s", pod.Name),
		}
		for k, v := range pod.Labels {
			selectorValues = append(selectorValues, fmt.Sprintf("pod-label:%s:%s", k, v))
		}
	}

	return selectorValues, nil
}

func (p *Plugin) validateServiceAccountToken(token string, log hclog.Logger) ([]string, error) {

	if token == "" {
		log.Warn("empty token")
		return nil, fmt.Errorf("empty token received ")
	}
	var podName string

	result, err := p.client.AuthenticationV1().TokenReviews().Create(context.Background(), &review.TokenReview{
		Spec: review.TokenReviewSpec{
			Token: token,
		},
	}, metav1.CreateOptions{})

	if err != nil || !result.Status.Authenticated {
		log.Warn("tokenReview failed: ", err)
		return nil, fmt.Errorf("tokenReview failed")
	}

	for key, value := range result.Status.User.Extra {
		if strings.Contains(key, "pod-name") {
			podName = value[0]
			break
		}
	}

	jwtToken, _, err := new(jwt.Parser).ParseUnverified(token, jwt.MapClaims{})
	if err != nil {
		log.Warn("token decoding failed ", err)
		return nil, err
	}
	kubernetesMap := jwtToken.Claims.(jwt.MapClaims)["kubernetes.io"].(map[string]interface{})

	podNs := kubernetesMap["namespace"].(string)

	return p.getSelectorValuesFromPodName(podName, podNs, log)
}
