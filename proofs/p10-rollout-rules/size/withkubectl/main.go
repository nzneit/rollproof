package main

import (
	"fmt"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/polymorphichelpers"
)

func main() {
	c, err := kubernetes.NewForConfig(&rest.Config{Host: "http://127.0.0.1:1"})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(c != nil, appsv1.SchemeGroupVersion)
	v, err := polymorphichelpers.StatusViewerFor(appsv1.SchemeGroupVersion.WithKind("Deployment").GroupKind())
	fmt.Println(v != nil, err)
}
