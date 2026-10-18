package kube

import (
	"context"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func ServiceDiagnostics(ctx context.Context, b Backend, selected unstructured.Unstructured) (DiagnosticReport, error) {
	r := DiagnosticReport{Title: "Service troubleshooting · " + selected.GetNamespace() + "/" + selected.GetName()}
	if selected.GetKind() != "Service" {
		return r, fmt.Errorf("select a Service for network diagnostics")
	}
	o, err := diagnosticTarget(ctx, b, selected)
	if err != nil {
		return r, err
	}
	var svc corev1.Service
	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, &svc); err != nil {
		return r, err
	}
	r.add("Service", fmt.Sprintf("Type: %s · Cluster IP: %s", svc.Spec.Type, svc.Spec.ClusterIP), "API observations only; DNS, routing and NetworkPolicy reachability are not probed.")
	if svc.Spec.Type == corev1.ServiceTypeExternalName {
		r.add("External name", svc.Spec.ExternalName, "ExternalName uses DNS rather than pod backends.")
		return r, nil
	}
	selector := labels.SelectorFromSet(svc.Spec.Selector).String()
	if selector == "" {
		r.add("Selector", "No selector: EndpointSlices may be managed manually; no namespace-wide pod scan is performed.")
	} else {
		r.add("Selector", selector)
	}
	if svc.Spec.PublishNotReadyAddresses {
		r.add("Readiness policy", "publishNotReadyAddresses=true: published endpoints may include unready pods.")
	}
	if svc.Spec.InternalTrafficPolicy != nil {
		r.add("Traffic policy", "internalTrafficPolicy="+string(*svc.Spec.InternalTrafficPolicy))
	}
	if svc.Spec.ExternalTrafficPolicy != "" {
		r.add("Traffic policy", "externalTrafficPolicy="+string(svc.Spec.ExternalTrafficPolicy))
	}
	var ports []string
	for _, p := range svc.Spec.Ports {
		target := p.TargetPort.String()
		if p.TargetPort == (intstr.IntOrString{}) {
			target = fmt.Sprint(p.Port)
		}
		ports = append(ports, fmt.Sprintf("%s %d/%s → %s", p.Name, p.Port, protocol(p.Protocol), target))
	}
	if len(ports) == 0 {
		ports = []string{"No ports declared."}
	}
	r.add("Port mappings", ports...)
	var pods []corev1.Pod
	podsKnown := selector != ""
	if podsKnown {
		pr, _ := Resolve(Builtins(), "pods")
		objects, e := b.List(ctx, Query{Resource: pr, Namespace: svc.Namespace, Labels: selector})
		if e != nil {
			podsKnown = false
			r.add("Matching pods · unavailable", e.Error())
		} else {
			var lines []string
			for i, o := range objects {
				if i >= 2000 {
					r.add("Limit", "Only the first 2,000 matching pods are analysed.")
					podsKnown = false
					break
				}
				var p corev1.Pod
				if e := runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, &p); e != nil {
					podsKnown = false
					continue
				}
				pods = append(pods, p)
				ready := false
				for _, c := range p.Status.Conditions {
					if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
						ready = true
					}
				}
				lines = append(lines, fmt.Sprintf("%s · %s · ready=%t · IP %s", p.Name, p.Status.Phase, ready, p.Status.PodIP))
				for _, sp := range svc.Spec.Ports {
					if sp.TargetPort.Type == intstr.String && !podHasPort(p, sp.TargetPort.StrVal, sp.Protocol) {
						lines = append(lines, "WARNING: "+p.Name+" has no container port named "+sp.TargetPort.StrVal+"/"+protocol(sp.Protocol))
					}
				}
			}
			if len(objects) == 0 {
				lines = append(lines, "WARNING: selector matches no pods in this namespace. Check pod labels and the Service selector.")
			}
			r.add(fmt.Sprintf("Matching pods (%d)", len(objects)), lines...)
		}
	}
	er := diagnosticResource("discovery.k8s.io", "v1", "endpointslices", "EndpointSlice", true)
	objects, e := b.List(ctx, Query{Resource: er, Namespace: svc.Namespace, Labels: labels.Set{"kubernetes.io/service-name": svc.Name}.AsSelector().String()})
	if e != nil {
		r.add("EndpointSlices · unavailable", e.Error(), "Ready backend count is unknown.")
		return r, ctx.Err()
	}
	seen := map[string]bool{}
	readyCount, total := 0, 0
	var lines []string
	for i, o := range objects {
		if i >= 2000 {
			lines = append(lines, "WARNING: only the first 2,000 EndpointSlices are analysed.")
			break
		}
		var es discoveryv1.EndpointSlice
		if e := runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, &es); e != nil {
			lines = append(lines, "Unavailable: "+e.Error())
			continue
		}
		wrongOwner := false
		for _, owner := range es.OwnerReferences {
			if owner.Kind == "Service" && owner.UID != svc.UID {
				wrongOwner = true
			}
		}
		if wrongOwner {
			lines = append(lines, "WARNING: "+es.Name+" refers to a different Service UID; omitted.")
			continue
		}
		var epPorts []string
		for _, p := range es.Ports {
			name := ""
			if p.Name != nil {
				name = *p.Name
			}
			port := "unset"
			if p.Port != nil {
				port = fmt.Sprint(*p.Port)
			}
			proto := "TCP"
			if p.Protocol != nil {
				proto = string(*p.Protocol)
			}
			epPorts = append(epPorts, fmt.Sprintf("%s=%s/%s", name, port, proto))
			matched := false
			for _, sp := range svc.Spec.Ports {
				if sp.Name == name && protocol(sp.Protocol) == proto {
					matched = true
					target := sp.TargetPort
					if target == (intstr.IntOrString{}) {
						target = intstr.FromInt32(sp.Port)
					}
					if target.Type == intstr.Int && p.Port != nil && target.IntVal != *p.Port {
						lines = append(lines, fmt.Sprintf("WARNING: %s endpoint port %d differs from targetPort %d", es.Name, *p.Port, target.IntVal))
					}
				}
			}
			if !matched {
				lines = append(lines, "WARNING: "+es.Name+" port "+name+"/"+proto+" has no matching Service port")
			}
		}
		slices.Sort(epPorts)
		if len(es.Ports) == 0 {
			lines = append(lines, "WARNING: "+es.Name+" has no endpoint ports")
		}
		for _, ep := range es.Endpoints {
			ready := ep.Conditions.Ready == nil || *ep.Conditions.Ready
			terminating := ep.Conditions.Terminating != nil && *ep.Conditions.Terminating
			serving := "unknown"
			if ep.Conditions.Serving != nil {
				serving = fmt.Sprint(*ep.Conditions.Serving)
			}
			podName := "external backend"
			if ep.TargetRef != nil && ep.TargetRef.Kind == "Pod" {
				podName = ep.TargetRef.Name
				if podsKnown {
					found := false
					for _, p := range pods {
						if p.Name == podName && (ep.TargetRef.Namespace == "" || ep.TargetRef.Namespace == p.Namespace) && p.UID == ep.TargetRef.UID {
							found = true
							for _, sp := range svc.Spec.Ports {
								if sp.TargetPort.Type != intstr.String {
									continue
								}
								actual := podPort(p, sp.TargetPort.StrVal, sp.Protocol)
								for _, port := range es.Ports {
									name := ""
									if port.Name != nil {
										name = *port.Name
									}
									proto := corev1.ProtocolTCP
									if port.Protocol != nil {
										proto = *port.Protocol
									}
									if actual != 0 && name == sp.Name && protocol(proto) == protocol(sp.Protocol) && port.Port != nil && *port.Port != actual {
										lines = append(lines, fmt.Sprintf("WARNING: %s endpoint port %d differs from %s container port %d", podName, *port.Port, sp.TargetPort.StrVal, actual))
									}
								}
							}
						}
					}
					if !found {
						lines = append(lines, "WARNING: "+podName+" is not a current selector-matching pod (check UID, labels and stale endpoints)")
					}
				}
			}
			for _, address := range ep.Addresses {
				key := string(es.AddressType) + "/" + address + "/" + strings.Join(epPorts, ",")
				if seen[key] {
					continue
				}
				seen[key] = true
				total++
				if ready {
					readyCount++
				}
				lines = append(lines, fmt.Sprintf("%s · %s · %s · ready=%t serving=%s terminating=%t", address, podName, strings.Join(epPorts, ", "), ready, serving, terminating))
			}
		}
	}
	if total == 0 {
		lines = append(lines, "WARNING: no published backends. Check selectors, pod readiness and EndpointSlice permissions/controller state.")
	} else if readyCount == 0 {
		lines = append(lines, "WARNING: no ready backends are published.")
	}
	r.add(fmt.Sprintf("EndpointSlices (%d) · %d/%d ready published addresses", len(objects), readyCount, total), lines...)
	r.add("Readiness notes", "An absent endpoint ready condition is interpreted as true. Terminating/serving flags are shown separately; traffic policy and publishNotReadyAddresses can affect actual routing.")
	return r, ctx.Err()
}
func protocol(p corev1.Protocol) string {
	if p == "" {
		return "TCP"
	}
	return string(p)
}
func podHasPort(p corev1.Pod, name string, proto corev1.Protocol) bool {
	return podPort(p, name, proto) != 0
}
func podPort(p corev1.Pod, name string, proto corev1.Protocol) int32 {
	containers := slices.Clone(p.Spec.Containers)
	for _, c := range p.Spec.InitContainers {
		if c.RestartPolicy != nil && *c.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			containers = append(containers, c)
		}
	}
	for _, c := range containers {
		for _, port := range c.Ports {
			if port.Name == name && protocol(port.Protocol) == protocol(proto) {
				return port.ContainerPort
			}
		}
	}
	return 0
}
