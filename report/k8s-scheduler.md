## 自定义priority

score计算公式

<a href="https://www.codecogs.com/eqnedit.php?latex=score&space;=&space;10-cpuFraction*5&space;-memoryFraction&space;*&space;5" target="_blank"><img src="https://latex.codecogs.com/gif.latex?score&space;=&space;10-cpuFraction*5&space;-memoryFraction&space;*&space;5" title="score = 10-cpuFraction*5 -memoryFraction * 5" /></a>

其中，cpuFraction 为

<a href="https://www.codecogs.com/eqnedit.php?latex=cpuFraction&space;=&space;\frac{Pod&space;Requested&space;Cpu}{Node&space;Allocable&space;Cpu}" target="_blank"><img src="https://latex.codecogs.com/gif.latex?cpuFraction&space;=&space;\frac{Pod&space;Requested&space;Cpu}{Node&space;Allocable&space;Cpu}" title="cpuFraction = \frac{Pod Requested Cpu}{Node Allocable Cpu}" /></a>

memoryFraction 

<a href="https://www.codecogs.com/eqnedit.php?latex=memoryFraction&space;=&space;\frac{Pod&space;Requested&space;Memory}{Node&space;Allocable&space;Memory}" target="_blank"><img src="https://latex.codecogs.com/gif.latex?memoryFraction&space;=&space;\frac{Pod&space;Requested&space;Memory}{Node&space;Allocable&space;Memory}" title="memoryFraction = \frac{Pod Requested Memory}{Node Allocable Memory}" /></a>

这个priority的目的主要是能够实现CPU和MEMORY的平均分配，同时将pod分配到资源最充足的node上。

与built-in的priority中LeastRequestedPriority相同之处在于资源空闲比越高的节点得分越高。

不同之处在于打分的机制不同。

## scheduler的原理

1. 首先，客户端通过API Server的REST API/kubectl/helm创建pod/service/deployment/job等，支持类型主要为JSON/YAML/helm tgz。
2. 接下来，API Server收到用户请求，存储到相关数据到etcd。
3. 调度器通过API Server查看未调度（bind）的Pod列表，循环遍历地为每个Pod分配节点，尝试为Pod分配节点。调度过程分为2个阶段：
   第一阶段：预选过程，过滤节点，调度器用一组规则过滤掉不符合要求的主机。比如Pod指定了所需要的资源量，那么可用资源比Pod需要的资源量少的主机会被过滤掉。
   第二阶段：优选过程，节点优先级打分，对第一步筛选出的符合要求的主机进行打分，在主机打分阶段，调度器会考虑一些整体优化策略，比如把容一个Replication Controller的副本分布到不同的主机上，使用最低负载的主机等。
4. 选择主机：选择打分最高的节点，进行binding操作，结果存储到etcd中。
5. 所选节点对于的kubelet根据调度结果执行Pod创建操作。 

## 自定义schedueler的实现过程

1. 定义接口，实现custom predicate和priority，在路由中注册好自定义的policy

   ```go
   Func: func(pod v1.Pod, node v1.Node) (bool, error) {
   			// get pod infos
   			podRequest_cpu := int64(0)
   			podRequest_mem :=int64(0)
   			for i := range pod.Spec.Containers {
   				container := &pod.Spec.Containers[i]
   				value_cpu := priorityutil.GetNonzeroRequestForResource(v1.ResourceCPU, &container.Resources.Requests)
   				value_mem :=  priorityutil.GetNonzeroRequestForResource(v1.ResourceMemory, &container.Resources.Requests)
   				podRequest_cpu += value_cpu
   				podRequest_mem += value_mem
   			}
   			// get node infos
   			allocableCPU :=  node.Status.Allocatable.Cpu().MilliValue()
   			allocableMemory := node.Status.Allocatable.Memory().MilliValue()
   			// compare
   			if float64(allocableCPU) > float64(podRequest_cpu) && float64(allocableMemory) > float64(podRequest_mem) {
   				return true, nil
   			}
   
   			return false, nil
   		}
   ```

   predicate是类似与podfitsrecourse的设计，只有能容纳pod的node才会被列为候选者。

   ```go
   func myscorer(requestmap, allocable ResourceToValueMap) int { 
   	cpuFraction := fractionOfCapacity(requestmap[v1.ResourceCPU], allocable[v1.ResourceCPU]) 
   	memoryFraction := fractionOfCapacity(requestmap[v1.ResourceMemory], allocable[v1.ResourceMemory])
   	if cpuFraction >= 1 || memoryFraction >= 1 {
   		// if requested >= capacity, the corresponding host should never be preferred.
   		return 0
   	}
   	return int(10-cpuFraction*5-memoryFraction*5) 
   }
   
   ```

2. 写好extender的yaml文件

   ```yaml
    "extenders" : [{
         "urlPrefix": "http://localhost/scheduler", 
         "filterVerb": "predicates/allocatable_true", //customized predicate
         "prioritizeVerb": "priorities/my_score", //customized priority
         "preemptVerb": "preemption",
         "bindVerb": "",
         "weight": 1,
         "enableHttps": false,
         "nodeCacheCapable": false
       }],
   ```

3. 打包成docker镜像，使用命令部署 

   ```
   sed 's/a\/b:c/'$(echo "${IMAGE}" | sed 's/\//\\\//')'/' extender.yaml | microk8s.kubectl apply -f -
   ```

4. 利用Pod的spec.schedulername字段来指定调度器 

   ```
   schedulerName: my-scheduler
   ```

5. 写ReplicaSet的yaml文件，并create ReplicaSet

   ```
   kubectl create -f test-pod.yaml
   ```

## workload

replicas: 10

app: nginx

requests:

cpu: "1"

memory: 1024Mi

## 测试环境

**Ubuntu Server 18.04 LTS (HVM), SSD Volume Type**

CPU:4

Memory:16

 共三台AWS

## 常用命令

```
microk8s.kubectl describe nodes
```

可以查看每个node的详细的资源分配信息

```
microk8s.kubectl top nodes
```

可以查看每个node的实时资源占用率

```
microk8s.kubectl get pods
```

可以得知每个pod的运行状态与运行时间

## 测试情况

实时情况

NAME               CPU(cores)   CPU%   MEMORY(bytes)   MEMORY%
ip-172-31-20-245   374m         9%     3955Mi          24%
ip-172-31-40-143   111m         2%     1037Mi          6%
ip-172-31-47-136   91m          2%     1023Mi          6%

每个pod的request占比情况（见node-status.txt）

## 结果分析

可以看出master结点的CPU和Memory占用率较高于slave结点。

slave结点之间的负载较为均衡。

## 优化空间

1. 在生产环境中，应当设置master节点默认拒绝将pod调度运行于其上的，这是防止master结点出现负载较高的情况。

2. 仅仅使用一种priority，只考虑资源空闲比是不够的。还需要考虑到locality、CPU和Memory之间的平衡等等策略。

   所以一些built-in的priority像是NodeAffinityPriority、BalancedAllocation就较好的解决了这些priority的计算问题。

# built-in scheduler的使用

首先使用yaml创建一个configmap，里面配置了我们想用的predicate和priority：

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: bult-in-policy
  namespace: kube-system
data:
 policy.cfg : |
  {
    "kind" : "Policy",
    "apiVersion" : "v1",
    "predicates" : [ 
      {"name" : "PodFitsResources"}
    ],
    "priorities" : [ 
      {"name" : "LeastRequestedPriority", "weight" : 1}
    ],
    "hardPodAffinitySymmetricWeight" : 10
  }
```

我们选用的predicate是PodFitsResources，资源足够的node将会进入候选名单。

priorities是LeastRequestedPriority，即选择接受pod后资源余量最多的node。

kubectl apply，创建该configmap，接着使用kube-scheduler指定默认的scheduler为我们刚刚创建的configmap。当pod未指定schedulerName时将使用此scheduler。

在两个nodes的集群中部署一个单位为5的replicaset，资源占用率如下：

node1:

![](./imgs/built-node1.PNG)

node2:

![](./imgs/built-node2.PNG)

两个node资源消耗分布均衡，符合预期。



# my-scheduler 与 built-in scheduler对比

采用控制变量法，replica只改变scheduler为my-scheduler，得到以下结果：

node1：

![](./imgs/my-node1.PNG)

node2:

![](./imgs/my-node2.PNG)

可见资源的消耗并不是很平均，但差得也不会太离谱。若要对cpu和memory进行更为平均的分配，还需要进一步优化原有算法，或者加入built-in scheduler。
