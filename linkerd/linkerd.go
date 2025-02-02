// Copyright 2019 Layer5.io
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package linkerd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	"github.com/alecthomas/template"
	"github.com/ghodss/yaml"
	"github.com/layer5io/meshery-linkerd/meshes"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)
// to solve
func (iClient *LinkerdClient) CreateMeshInstance(_ context.Context, k8sReq *meshes.CreateMeshInstanceRequest) (*meshes.CreateMeshInstanceResponse, error) { //to solve
	var k8sConfig []byte //to solve
	contextName := "" //to solve
	if k8sReq != nil { //to solve
		k8sConfig = k8sReq.K8SConfig  //to solve
		contextName = k8sReq.ContextName //to solve
	} //to solve
	// logrus.Debugf("received k8sConfig: %s", k8sConfig)  //to solve
	logrus.Debugf("received contextName: %s", contextName) //to solve

	ic, err := newClient(k8sConfig, contextName) //to solve
	if err != nil { //to solve
		err = errors.Wrapf(err, "unable to create a new linkerd client") //to solve
		logrus.Error(err) //to solve
		return nil, err //to solve
	}
	iClient.k8sClientset = ic.k8sClientset //to solve
	iClient.k8sDynamicClient = ic.k8sDynamicClient //to solve
	iClient.eventChan = make(chan *meshes.EventsResponse, 100) //to solve
	iClient.config = ic.config //to solve
	iClient.contextName = ic.contextName //to solve
	iClient.kubeconfig = ic.kubeconfig //to solve
	return &meshes.CreateMeshInstanceResponse{}, nil //to solve
} //to solve

func (iClient *LinkerdClient) createResource(ctx context.Context, res schema.GroupVersionResource, data *unstructured.Unstructured) error {
	_, err := iClient.k8sDynamicClient.Resource(res).Namespace(data.GetNamespace()).Create(data, metav1.CreateOptions{})
	if err != nil {
		err = errors.Wrapf(err, "unable to create the requested resource, attempting operation without namespace")
		logrus.Warn(err)
		_, err = iClient.k8sDynamicClient.Resource(res).Create(data, metav1.CreateOptions{})
		if err != nil {
			err = errors.Wrapf(err, "unable to create the requested resource, attempting to update")
			logrus.Error(err)
			return err
		}
	}
	logrus.Infof("Created Resource of type: %s and name: %s", data.GetKind(), data.GetName())
	return nil
}

func (iClient *LinkerdClient) deleteResource(ctx context.Context, res schema.GroupVersionResource, data *unstructured.Unstructured) error {
	if iClient.k8sDynamicClient == nil {
		return errors.New("mesh client has not been created")
	}

	if res.Resource == "namespaces" && data.GetName() == "default" { // skipping deletion of default namespace
		return nil
	}

	// in the case with deployments, have to scale it down to 0 first and then delete. . . or else RS and pods will be left behind
	if res.Resource == "deployments" {
		data1, err := iClient.getResource(ctx, res, data)
		if err != nil {
			return err
		}
		depl := data1.UnstructuredContent()
		spec1 := depl["spec"].(map[string]interface{})
		spec1["replicas"] = 0
		data1.SetUnstructuredContent(depl)
		if err = iClient.updateResource(ctx, res, data1); err != nil {
			return err
		}
	}

	err := iClient.k8sDynamicClient.Resource(res).Namespace(data.GetNamespace()).Delete(data.GetName(), &metav1.DeleteOptions{})
	if err != nil {
		err = errors.Wrapf(err, "unable to delete the requested resource, attempting operation without namespace")
		logrus.Warn(err)

		err := iClient.k8sDynamicClient.Resource(res).Delete(data.GetName(), &metav1.DeleteOptions{})
		if err != nil {
			err = errors.Wrapf(err, "unable to delete the requested resource")
			logrus.Error(err)
			return err
		}
	}
	logrus.Infof("Deleted Resource of type: %s and name: %s", data.GetKind(), data.GetName())
	return nil
}

func (iClient *LinkerdClient) getResource(ctx context.Context, res schema.GroupVersionResource, data *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	var data1 *unstructured.Unstructured
	var err error
	logrus.Debugf("getResource: %+#v", data)
	data1, err = iClient.k8sDynamicClient.Resource(res).Namespace(data.GetNamespace()).Get(data.GetName(), metav1.GetOptions{})
	if err != nil {
		err = errors.Wrap(err, "unable to retrieve the resource with a matching name, attempting operation without namespace")
		logrus.Warn(err)

		data1, err = iClient.k8sDynamicClient.Resource(res).Get(data.GetName(), metav1.GetOptions{})
		if err != nil {
			err = errors.Wrap(err, "unable to retrieve the resource with a matching name, while attempting to apply the config")
			logrus.Error(err)
			return nil, err
		}
	}
	logrus.Infof("Retrieved Resource of type: %s and name: %s", data.GetKind(), data.GetName())
	return data1, nil
}

func (iClient *LinkerdClient) updateResource(ctx context.Context, res schema.GroupVersionResource, data *unstructured.Unstructured) error {
	if _, err := iClient.k8sDynamicClient.Resource(res).Namespace(data.GetNamespace()).Update(data, metav1.UpdateOptions{}); err != nil {
		err = errors.Wrap(err, "unable to update resource with the given name, attempting operation without namespace")
		logrus.Warn(err)

		if _, err = iClient.k8sDynamicClient.Resource(res).Update(data, metav1.UpdateOptions{}); err != nil {
			err = errors.Wrap(err, "unable to update resource with the given name, while attempting to apply the config")
			logrus.Error(err)
			return err
		}
	}
	logrus.Infof("Updated Resource of type: %s and name: %s", data.GetKind(), data.GetName())
	return nil
}

// MeshName just returns the name of the mesh the client is representing
func (iClient *LinkerdClient) MeshName(context.Context, *meshes.MeshNameRequest) (*meshes.MeshNameResponse, error) {
	return &meshes.MeshNameResponse{Name: "Linkerd"}, nil
}

func (iClient *LinkerdClient) applyRulePayload(ctx context.Context, namespace string, newBytes []byte, delete bool) error {
	if iClient.k8sDynamicClient == nil {
		return errors.New("mesh client has not been created")
	}
	jsonBytes, err := yaml.YAMLToJSON(newBytes)
	if err != nil {
		err = errors.Wrapf(err, "unable to convert yaml to json")
		logrus.Errorf("received yaml bytes: %s", newBytes)
		logrus.Error(err)
		return err
	}
	// logrus.Debugf("created json: %s, length: %d", jsonBytes, len(jsonBytes))
	if len(jsonBytes) > 5 { // attempting to skip 'null' json
		data := &unstructured.Unstructured{}
		err = data.UnmarshalJSON(jsonBytes)
		if err != nil {
			err = errors.Wrapf(err, "unable to unmarshal json created from yaml")
			logrus.Error(err)
			logrus.Errorf("received yaml bytes: %s", newBytes)
			return err
		}
		if data.IsList() {
			err = data.EachListItem(func(r runtime.Object) error {
				dataL, _ := r.(*unstructured.Unstructured)
				return iClient.executeRule(ctx, dataL, namespace, delete)
			})
			return err
		}
		return iClient.executeRule(ctx, data, namespace, delete)
	}
	return nil
}

func (iClient *LinkerdClient) executeRule(ctx context.Context, data *unstructured.Unstructured, namespace string, delete bool) error {
	// logrus.Debug("========================================================")
	// logrus.Debugf("Received data: %+#v", data)
	if namespace != "" {
		data.SetNamespace(namespace)
	}
	groupVersion := strings.Split(data.GetAPIVersion(), "/")
	logrus.Debugf("groupVersion: %v", groupVersion)
	var group, version string
	if len(groupVersion) == 2 {
		group = groupVersion[0]
		version = groupVersion[1]
	} else if len(groupVersion) == 1 {
		version = groupVersion[0]
	}

	kind := strings.ToLower(data.GetKind())
	switch kind {
	case "logentry":
		kind = "logentries"
	case "kubernetes":
		kind = "kuberneteses"
	default:
		kind += "s"
	}

	res := schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: kind,
	}
	logrus.Debugf("Computed Resource: %+#v", res)

	if delete {
		return iClient.deleteResource(ctx, res, data)
	}

	if err := iClient.createResource(ctx, res, data); err != nil {
		data1, err := iClient.getResource(ctx, res, data)
		if err != nil {
			return err
		}
		if err = iClient.updateResource(ctx, res, data1); err != nil {
			return err
		}
	}
	return nil
}

func (iClient *LinkerdClient) labelNamespaceForAutoInjection(ctx context.Context, namespace string) error {
	ns := &unstructured.Unstructured{}
	res := schema.GroupVersionResource{
		Version:  "v1",
		Resource: "namespaces",
	}
	ns.SetName(namespace)
	ns, err := iClient.getResource(ctx, res, ns)
	if err != nil {
		if strings.HasSuffix(err.Error(), "not found") {
			if err = iClient.createNamespace(ctx, namespace); err != nil {
				return err
			}

			ns := &unstructured.Unstructured{}
			ns.SetName(namespace)
			ns, err = iClient.getResource(ctx, res, ns)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	logrus.Debugf("retrieved namespace: %+#v", ns)
	if ns == nil {
		ns = &unstructured.Unstructured{}
		ns.SetName(namespace)
	}
	ns.SetAnnotations(map[string]string{
		"linkerd.io/inject": "enabled",
	})
	err = iClient.updateResource(ctx, res, ns)
	if err != nil {
		return err
	}
	return nil
}

func (iClient *LinkerdClient) executeInstall(ctx context.Context, arReq *meshes.ApplyRuleRequest) error {
	var tmpKubeConfigFileLoc = path.Join(os.TempDir(), fmt.Sprintf("kubeconfig_%d", time.Now().UnixNano()))
	// -l <namespace> --context <context name> --kubeconfig <file path>
	// logrus.Debugf("about to write kubeconfig to file: %s", iClient.kubeconfig)
	if err := ioutil.WriteFile(tmpKubeConfigFileLoc, iClient.kubeconfig, 0700); err != nil {
		return err
	}
	// defer os.Remove(tmpKubeConfigFileLoc)

	args1 := []string{"-l", arReq.Namespace}
	if iClient.contextName != "" {
		args1 = append(args1, "--context", iClient.contextName)
	}
	args1 = append(args1, "--kubeconfig", tmpKubeConfigFileLoc)

	preCheck := append(args1, "check", "--pre")
	yamlFileContents, er, err := iClient.execute(preCheck...)
	if err != nil {
		return err
	}

	installArgs := append(args1, "install", "--ignore-cluster")
	yamlFileContents, er, err = iClient.execute(installArgs...)
	if err != nil {
		return err
	}
	if er != "" {
		err = fmt.Errorf("received error while attempting to prepare install yaml: %s", er)
		logrus.Error(err)
		return err
	}
	if err := iClient.applyConfigChange(ctx, yamlFileContents, arReq.Namespace, arReq.DeleteOp); err != nil {
		return err
	}
	return nil
}

func (iClient *LinkerdClient) executeTemplate(ctx context.Context, username, namespace, templateName string) (string, error) {
	tmpl, err := template.ParseFiles(path.Join("linkerd", "config_templates", templateName))
	if err != nil {
		err = errors.Wrapf(err, "unable to parse template")
		logrus.Error(err)
		return "", err
	}
	buf := bytes.NewBufferString("")
	err = tmpl.Execute(buf, map[string]string{
		"user_name": username,
		"namespace": namespace,
	})
	if err != nil {
		err = errors.Wrapf(err, "unable to execute template")
		logrus.Error(err)
		return "", err
	}
	return buf.String(), nil
}

func (iClient *LinkerdClient) createNamespace(ctx context.Context, namespace string) error {
	logrus.Debugf("creating namespace: %s", namespace)
	yamlFileContents, err := iClient.executeTemplate(ctx, "", namespace, "namespace.yml")
	if err != nil {
		return err
	}
	if err := iClient.applyConfigChange(ctx, yamlFileContents, namespace, false); err != nil {
		return err
	}
	return nil
}

// ApplyRule is a method invoked to apply a particular operation on the mesh in a namespace
func (iClient *LinkerdClient) ApplyOperation(ctx context.Context, arReq *meshes.ApplyRuleRequest) (*meshes.ApplyRuleResponse, error) {
	if arReq == nil {
		return nil, errors.New("mesh client has not been created")
	}

	op, ok := supportedOps[arReq.OpName]
	if !ok {
		return nil, fmt.Errorf("operation id: %s, error: %s is not a valid operation name", arReq.OperationId, arReq.OpName)
	}

	if arReq.OpName == customOpCommand && arReq.CustomBody == "" {
		return nil, fmt.Errorf("operation id: %s, error: yaml body is empty for %s operation", arReq.OperationId, arReq.OpName)
	}

	var yamlFileContents string
	var appName, svcName string
	var err error

	switch arReq.OpName {
	case customOpCommand:
		yamlFileContents = arReq.CustomBody
	case installLinkerdCommand:
		go func() {
			opName1 := "deploying"
			if arReq.DeleteOp {
				opName1 = "removing"
			}
			if err := iClient.executeInstall(ctx, arReq); err != nil {
				iClient.eventChan <- &meshes.EventsResponse{
					OperationId: arReq.OperationId,
					EventType:   meshes.EventType_ERROR,
					Summary:     fmt.Sprintf("Error while %s Linkerd", opName1),
					Details:     err.Error(),
				}
				return
			}
			opName := "deployed"
			if arReq.DeleteOp {
				opName = "removed"
			}
			iClient.eventChan <- &meshes.EventsResponse{
				OperationId: arReq.OperationId,
				EventType:   meshes.EventType_INFO,
				Summary:     fmt.Sprintf("Linkerd %s successfully", opName),
				Details:     fmt.Sprintf("The latest version of Linkerd is now %s.", opName),
			}
			return
		}()
		return &meshes.ApplyRuleResponse{
			OperationId: arReq.OperationId,
		}, nil
	case installBooksAppCommand:
		appName = "Linkerd Books App"
		svcName = "webapp"
		yamlFileContents, err = iClient.getYAML(booksAppInstallFile, booksAppLocalFile)
		if err != nil {
			return nil, err
		}
		fallthrough
	case installHTTPBinApp:
		if appName == "" {
			appName = "HTTP Bin App"
			svcName = "httpbin"
			yamlFileContents, err = iClient.executeTemplate(ctx, arReq.Username, arReq.Namespace, op.templateName)
			if err != nil {
				return nil, err
			}
		}
		fallthrough
	case installIstioBookInfoApp:
		if appName == "" {
			appName = "Istio canonical Book Info App"
			svcName = "productpage"
			yamlFileContents, err = iClient.executeTemplate(ctx, arReq.Username, arReq.Namespace, op.templateName)
			if err != nil {
				return nil, err
			}
		}
		fallthrough
	case installEmojiVotoCommand:
		if appName == "" {
			appName = "Emojivoto App"
			svcName = "web-svc"
			yamlFileContents, err = iClient.getYAML(emojivotoInstallFile, emojivotoLocalFile)
			if err != nil {
				return nil, err
			}
		}
		go func() {
			opName1 := "deploying"
			if arReq.DeleteOp {
				opName1 = "removing"
			}
			if !arReq.DeleteOp {
				if err := iClient.labelNamespaceForAutoInjection(ctx, arReq.Namespace); err != nil {
					iClient.eventChan <- &meshes.EventsResponse{
						OperationId: arReq.OperationId,
						EventType:   meshes.EventType_ERROR,
						Summary:     fmt.Sprintf("Error while %s the canonical %s", opName1, appName),
						Details:     err.Error(),
					}
					return
				}
			}
			if err := iClient.applyConfigChange(ctx, yamlFileContents, arReq.Namespace, arReq.DeleteOp); err != nil {
				iClient.eventChan <- &meshes.EventsResponse{
					OperationId: arReq.OperationId,
					EventType:   meshes.EventType_ERROR,
					Summary:     fmt.Sprintf("Error while %s the canonical %s", opName1, appName),
					Details:     err.Error(),
				}
				return
			}
			opName := "deployed"
			ports := []int64{}
			if arReq.DeleteOp {
				opName = "removed"
			} else {
				var err error
				ports, err = iClient.getSVCPort(ctx, svcName, arReq.Namespace)
				if err != nil {
					iClient.eventChan <- &meshes.EventsResponse{
						OperationId: arReq.OperationId,
						EventType:   meshes.EventType_WARN,
						Summary:     fmt.Sprintf("%s is deployed but unable to retrieve the port info for the service at the moment", appName),
						Details:     err.Error(),
					}
					return
				}
			}
			var portMsg string
			if len(ports) == 1 {
				portMsg = fmt.Sprintf("The service is possibly available on port: %v", ports)
			} else if len(ports) > 1 {
				portMsg = fmt.Sprintf("The service is possibly available on one of the following ports: %v", ports)
			}
			msg := fmt.Sprintf("%s is now %s. %s", appName, opName, portMsg)
			iClient.eventChan <- &meshes.EventsResponse{
				OperationId: arReq.OperationId,
				EventType:   meshes.EventType_INFO,
				Summary:     fmt.Sprintf("%s %s successfully", appName, opName),
				Details:     msg,
			}
			return
		}()
		return &meshes.ApplyRuleResponse{
			OperationId: arReq.OperationId,
		}, nil
	default:
		// tmpl, err := template.ParseFiles(path.Join("linkerd", "config_templates", op.templateName))
		// if err != nil {
		// 	err = errors.Wrapf(err, "unable to parse template")
		// 	logrus.Error(err)
		// 	return nil, err
		// }
		// buf := bytes.NewBufferString("")
		// err = tmpl.Execute(buf, map[string]string{
		// 	"user_name": arReq.Username,
		// 	"namespace": arReq.Namespace,
		// })
		// if err != nil {
		// 	err = errors.Wrapf(err, "unable to execute template")
		// 	logrus.Error(err)
		// 	return nil, err
		// }
		// yamlFileContents = buf.String()
		err := fmt.Errorf("please select a valid operation")
		logrus.Error(err)
		return nil, err
	}

	if err := iClient.applyConfigChange(ctx, yamlFileContents, arReq.Namespace, arReq.DeleteOp); err != nil {
		return nil, err
	}

	return &meshes.ApplyRuleResponse{
		OperationId: arReq.OperationId,
	}, nil
}

func (iClient *LinkerdClient) applyConfigChange(ctx context.Context, yamlFileContents, namespace string, delete bool) error {
	// yamls := strings.Split(yamlFileContents, "---")
	yamls, err := iClient.splitYAML(yamlFileContents)
	if err != nil {
		err = errors.Wrap(err, "error while splitting yaml")
		logrus.Error(err)
		return err
	}
	for _, yml := range yamls {
		// if strings.TrimSpace(yml) != "" {
		if err := iClient.applyRulePayload(ctx, namespace, []byte(yml), delete); err != nil {
			errStr := strings.TrimSpace(err.Error())
			if delete && (strings.HasSuffix(errStr, "not found") ||
				strings.HasSuffix(errStr, "the server could not find the requested resource")) {
				// logrus.Debugf("skipping error. . .")
				continue
			}
			// logrus.Debugf("returning error: %v", err)
			return err
		}
		// }
	}
	return nil
}

// SupportedOperations - returns a list of supported operations on the mesh
func (iClient *LinkerdClient) SupportedOperations(context.Context, *meshes.SupportedOperationsRequest) (*meshes.SupportedOperationsResponse, error) {
	supportedOpsCount := len(supportedOps)
	result := make([]*meshes.SupportedOperation, supportedOpsCount)
	i := 0
	for k, sp := range supportedOps {
		result[i] = &meshes.SupportedOperation{
			Key:      k,
			Value:    sp.name,
			Category: sp.opType,
		}
		i++
	}
	return &meshes.SupportedOperationsResponse{
		Ops: result,
	}, nil
}

// StreamEvents - streams generated/collected events to the client
func (iClient *LinkerdClient) StreamEvents(in *meshes.EventsRequest, stream meshes.MeshService_StreamEventsServer) error {
	logrus.Debugf("waiting on event stream. . .")
	for {
		select {
		case event := <-iClient.eventChan:
			logrus.Debugf("sending event: %+#v", event)
			if err := stream.Send(event); err != nil {
				err = errors.Wrapf(err, "unable to send event")

				// to prevent loosing the event, will re-add to the channel
				go func() {
					iClient.eventChan <- event
				}()
				logrus.Error(err)
				return err
			}
		default:
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

func (iClient *LinkerdClient) splitYAML(yamlContents string) ([]string, error) {
	yamlDecoder, ok := NewDocumentDecoder(ioutil.NopCloser(bytes.NewReader([]byte(yamlContents)))).(*YAMLDecoder)
	if !ok {
		err := fmt.Errorf("unable to create a yaml decoder")
		logrus.Error(err)
		return nil, err
	}
	defer yamlDecoder.Close()
	var err error
	n := 0
	data := [][]byte{}
	ind := 0
	for err == io.ErrShortBuffer || err == nil {
		// for {
		d := make([]byte, 1000)
		n, err = yamlDecoder.Read(d)
		// logrus.Debugf("Read this: %s, count: %d, err: %v", d, n, err)
		if len(data) == 0 || len(data) <= ind {
			data = append(data, []byte{})
		}
		if n > 0 {
			data[ind] = append(data[ind], d...)
		}
		if err == nil {
			logrus.Debugf("..............BOUNDARY................")
			ind++
		}
	}
	result := make([]string, len(data))
	for i, row := range data {
		r := string(row)
		r = strings.Trim(r, "\x00")
		logrus.Debugf("ind: %d, data: %s", i, r)
		result[i] = r
	}
	return result, nil
}

func (iClient *LinkerdClient) getSVCPort(ctx context.Context, svc, namespace string) ([]int64, error) {
	// web-svc
	ns := &unstructured.Unstructured{}
	res := schema.GroupVersionResource{
		Version:  "v1",
		Resource: "services",
	}
	ns.SetName(svc)
	ns.SetNamespace(namespace)
	ns, err := iClient.getResource(ctx, res, ns)
	if err != nil {
		err = errors.Wrapf(err, "unable to get service details")
		logrus.Error(err)
		return nil, err
	}
	svcInst := ns.UnstructuredContent()
	spec := svcInst["spec"].(map[string]interface{})
	ports, _ := spec["ports"].([]interface{})
	nodePorts := []int64{}
	for _, port := range ports {
		p, _ := port.(map[string]interface{})
		np, ok := p["nodePort"]
		if ok {
			npi, _ := np.(int64)
			nodePorts = append(nodePorts, npi)
		}
	}
	logrus.Debugf("retrieved svc: %+#v", ns)
	return nodePorts, nil
}
