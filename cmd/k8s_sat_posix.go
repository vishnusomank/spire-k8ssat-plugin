package k8ssat

import (
	"context"
	"errors"
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	rest "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
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

	read, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
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

// =============== //
// == Namespace == //
// =============== //

func GetNamespacesFromK8sClient() []string {
	results := []string{}

	client := ConnectK8sClient()
	if client == nil {
		return results
	}

	// get namespaces from k8s api client
	namespaces, err := client.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Error().Msg(err.Error())
		return results
	}

	for _, namespace := range namespaces.Items {
		if namespace.Status.Phase != "Active" {
			continue
		}

		results = append(results, namespace.Name)
	}

	return results
}

// ========= //
// == Pod == //
// ========= //

func GetPodsFromK8sClient() *v1.PodList {

	client := ConnectK8sClient()
	if client == nil {
		return nil
	}

	// get pods from k8s api client
	pods, err := client.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Error().Msg(err.Error())
		return &v1.PodList{}
	}
	return pods
}

func SetAnnotationsToPodsInNamespaceK8s(namespace string, annotation map[string]string) error {
	client := ConnectK8sClient()
	if client == nil {
		return errors.New("no client")
	}

	// get pods from k8s api client
	pods, err := client.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		copied := pod.DeepCopy()
		ann := copied.ObjectMeta.Annotations
		if ann == nil {
			ann = make(map[string]string)
		}
		for k, v := range annotation {
			ann[k] = v
		}
		copied.SetAnnotations(ann)
		_, err := client.CoreV1().Pods(copied.ObjectMeta.Namespace).Update(context.Background(), copied, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}

func SetAnnotationsToPodK8s(podName string, annotation map[string]string) error {
	client := ConnectK8sClient()
	if client == nil {
		return errors.New("no client")
	}

	// get pods from k8s api client
	pods, err := client.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		if pod.Name == podName {
			copied := pod.DeepCopy()
			ann := copied.ObjectMeta.Annotations
			if ann == nil {
				ann = make(map[string]string)
			}
			for k, v := range annotation {
				ann[k] = v
			}
			copied.SetAnnotations(ann)
			_, err := client.CoreV1().Pods(copied.ObjectMeta.Namespace).Update(context.Background(), copied, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		}
	}

	return nil
}