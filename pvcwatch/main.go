package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	var ns, label, field, maxClaims string
	flag.StringVar(&ns, "namespace", "", "namespace")
	flag.StringVar(&label, "l", "", "Label selector")
	flag.StringVar(&field, "f", "", "Field selector")
	flag.StringVar(&maxClaims, "max-claims", "200Gi", "Maximum total claims to watch")
	flag.Parse()

	var totalClaimedQuant resource.Quantity
	maxClaimedQuant := resource.MustParse(maxClaims)

	// bootstrap config
	fmt.Println()
	kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	fmt.Println("Using kubeconfig: ", kubeconfig)
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	// initial list
	listOptions := metav1.ListOptions{LabelSelector: label, FieldSelector: field}
	pvcs, err := clientset.CoreV1().PersistentVolumeClaims(ns).List(listOptions)
	if err != nil {
		log.Fatal(err)
	}

	printPVCs(pvcs)
	fmt.Println()

	// watch future changes to PVCs
	watcher, err := clientset.CoreV1().PersistentVolumeClaims(ns).Watch(listOptions)
	if err != nil {
		log.Fatal(err)
	}
	ch := watcher.ResultChan()

	fmt.Printf("--- PVC Watch (max claims %v) ----\n", maxClaimedQuant.String())
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				log.Fatal("watcher channel closed")
			}

			pvc, ok := event.Object.(*v1.PersistentVolumeClaim)
			if !ok {
				log.Fatal("unexpected type")
			}
			quant := pvc.Spec.Resources.Requests[v1.ResourceStorage]

			switch event.Type {
			case watch.Added:
				totalClaimedQuant.Add(quant)
				log.Printf("PVC %s added, claim size %s\n", pvc.Name, quant.String())
				log.Printf("\nAt %3.1f%% claim capcity\n",
					float64(totalClaimedQuant.Value())/float64(maxClaimedQuant.Value())*100,
				)
				// is claim overage?
				if totalClaimedQuant.Cmp(maxClaimedQuant) > 1 {
					log.Printf("Claim overage reached: max %v got %v",
						maxClaimedQuant.String(),
						totalClaimedQuant.String(),
					)
					// trigger action
					log.Println("*** Taking action ***")
				}

			case watch.Modified:
				//log.Printf("Pod %s modified\n", pod.GetName())
			case watch.Deleted:
				quant := pvc.Spec.Resources.Requests[v1.ResourceStorage]
				totalClaimedQuant.Sub(quant)
				log.Printf("PVC %s removed, size %s\n", pvc.Name, quant.String())
				log.Printf("\nAt %3.1f%% claim capcity\n",
					float64(totalClaimedQuant.Value())/float64(maxClaimedQuant.Value())*100,
				)
			case watch.Error:
				//log.Printf("watcher error encountered\n", pod.GetName())
			}

		}

	}
}

// printPVCs prints a list of PersistentVolumeClaim on console
func printPVCs(pvcs *v1.PersistentVolumeClaimList) {
	if len(pvcs.Items) == 0 {
		log.Println("No claims found")
		return
	}
	template := "%-32s%-8s%-8s\n"
	fmt.Println("--- PVCs ----")
	fmt.Printf(template, "NAME", "STATUS", "CAPACITY")
	var cap resource.Quantity
	for _, pvc := range pvcs.Items {
		quant := pvc.Spec.Resources.Requests[v1.ResourceStorage]
		cap.Add(quant)
		fmt.Printf(template, pvc.Name, string(pvc.Status.Phase), quant.String())
	}

	fmt.Println("-----------------------------")
	fmt.Printf("Total capacity claimed: %s\n", cap.String())
	fmt.Println("-----------------------------")
}
