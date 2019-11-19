package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/comail/colog"
	"github.com/julienschmidt/httprouter"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	schedulerapi "k8s.io/kubernetes/pkg/scheduler/api" 
	priorityutil "k8s.io/kubernetes/pkg/scheduler/algorithm/priorities/util"
	"strconv"
)

const (
	versionPath      = "/version"
	apiPrefix        = "/scheduler"
	bindPath         = apiPrefix + "/bind"
	preemptionPath   = apiPrefix + "/preemption"
	predicatesPrefix = apiPrefix + "/predicates"
	prioritiesPrefix = apiPrefix + "/priorities"
)

type ResourceToValueMap map[v1.ResourceName]int64

func myscorer(requestmap, allocable ResourceToValueMap) int { 
	cpuFraction := fractionOfCapacity(requestmap[v1.ResourceCPU], allocable[v1.ResourceCPU]) 
	memoryFraction := fractionOfCapacity(requestmap[v1.ResourceMemory], allocable[v1.ResourceMemory])
	if cpuFraction >= 1 || memoryFraction >= 1 {
		// if requested >= capacity, the corresponding host should never be preferred.
		return 0
	}
	return int(10-cpuFraction-memoryFraction) 
}

func fractionOfCapacity(requested, capacity int64) float64 {
	if capacity == 0 {
		return 1
	}
	return float64(requested) / float64(capacity)
}

var (
	version string // injected via ldflags at build time

	TruePredicate = Predicate{
		Name: "always_true",
		Func: func(pod v1.Pod, node v1.Node) (bool, error) {
			return true, nil
		},
	} 


	myPriority = Prioritize{
		Name: "my_score",//allocate by CPU 
		Func: func(pod v1.Pod, nodes []v1.Node) (*schedulerapi.HostPriorityList, error) {
			var priorityList schedulerapi.HostPriorityList
			priorityList = make([]schedulerapi.HostPriority, len(nodes))
			podRequest_cpu := int64(0)
			podRequest_mem :=int64(0)
			for i := range pod.Spec.Containers {
				container := &pod.Spec.Containers[i]
				value_cpu := priorityutil.GetNonzeroRequestForResource(v1.ResourceCPU, &container.Resources.Requests)
				value_mem :=  priorityutil.GetNonzeroRequestForResource(v1.ResourceMemory, &container.Resources.Requests)
				podRequest_cpu += value_cpu
				podRequest_mem += value_mem
			}
			requestmap := make(ResourceToValueMap,2)
			requestmap[v1.ResourceCPU] = podRequest_cpu   
			requestmap[v1.ResourceMemory] = podRequest_mem

			log.Print("pod cpu request  ", strconv.FormatInt( podRequest_cpu   ,10) )  
			
			log.Print("pod memory request  ", strconv.FormatInt( podRequest_mem   ,10) )  
			
			allocable_map := make(ResourceToValueMap,len(nodes)*2)
			for i, node := range nodes {
				allocable_map[v1.ResourceCPU] =  node.Status.Allocatable.Cpu().MilliValue()
				allocable_map[v1.ResourceMemory] = node.Status.Allocatable.Memory().MilliValue()

				log.Print("node has cpu " ,strconv.FormatInt(allocable_map[v1.ResourceCPU]   ,10))  
				log.Print("node has memory " ,strconv.FormatInt( allocable_map[v1.ResourceMemory]  ,10))   

				priorityList[i] = schedulerapi.HostPriority{
					Host:  node.Name,
					Score: myscorer(requestmap,allocable_map),
				}
			}
			return &priorityList, nil
		},
	}

	NoBind = Bind{
		Func: func(podName string, podNamespace string, podUID types.UID, node string) error {
			return fmt.Errorf("This extender doesn't support Bind.  Please make 'BindVerb' be empty in your ExtenderConfig.")
		},
	}

	EchoPreemption = Preemption{
		Func: func(
			_ v1.Pod,
			_ map[string]*schedulerapi.Victims,
			nodeNameToMetaVictims map[string]*schedulerapi.MetaVictims,
		) map[string]*schedulerapi.MetaVictims {
			return nodeNameToMetaVictims
		},
	}
)

func StringToLevel(levelStr string) colog.Level {
	switch level := strings.ToUpper(levelStr); level {
	case "TRACE":
		return colog.LTrace
	case "DEBUG":
		return colog.LDebug
	case "INFO":
		return colog.LInfo
	case "WARNING":
		return colog.LWarning
	case "ERROR":
		return colog.LError
	case "ALERT":
		return colog.LAlert
	default:
		log.Printf("warning: LOG_LEVEL=\"%s\" is empty or invalid, fallling back to \"INFO\".\n", level)
		return colog.LInfo
	}
}

func main() {
	colog.SetDefaultLevel(colog.LInfo)
	colog.SetMinLevel(colog.LInfo)
	colog.SetFormatter(&colog.StdFormatter{
		Colors: true,
		Flag:   log.Ldate | log.Ltime | log.Lshortfile,
	})
	colog.Register()
	level := StringToLevel(os.Getenv("LOG_LEVEL"))
	log.Print("Log level was set to ", strings.ToUpper(level.String()))
	colog.SetMinLevel(level)

	router := httprouter.New()
	AddVersion(router)

	predicates := []Predicate{TruePredicate}
	for _, p := range predicates {
		AddPredicate(router, p)
	}

	priorities := []Prioritize{myPriority}
	for _, p := range priorities {
		AddPrioritize(router, p)
	}

	AddBind(router, NoBind)

	log.Print("info: server starting on the port :80")
	if err := http.ListenAndServe(":80", router); err != nil {
		log.Fatal(err)
	}
}
