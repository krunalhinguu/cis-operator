package core

import (
	"bytes"
	"encoding/json"
	"text/template"

	"github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8Yaml "k8s.io/apimachinery/pkg/util/yaml"

	wcorev1 "github.com/rancher/wrangler/pkg/generated/controllers/core/v1"
	"github.com/rancher/wrangler/pkg/name"

	cisoperatorapiv1 "github.com/rancher/cis-operator/pkg/apis/cis.cattle.io/v1"
)

type OverrideSkipInfoData struct {
	Skip map[string][]string `json:"skip"`
}

const (
	CurrentBenchmarkKey    = "current"
	ConfigFileName         = "config.json"
	customBenchmarkBaseDir = "/etc/kbs/custombenchmark/cfg"
)

func NewConfigMaps(clusterscan *cisoperatorapiv1.ClusterScan, clusterscanprofile *cisoperatorapiv1.ClusterScanProfile, clusterscanbenchmark *cisoperatorapiv1.ClusterScanBenchmark, controllerName string, imageConfig *cisoperatorapiv1.ScanImageConfig, configmapsClient wcorev1.ConfigMapController) (cmMap map[string]*corev1.ConfigMap, err error) {
	cmMap = make(map[string]*corev1.ConfigMap)

	configdata := map[string]interface{}{
		"namespace":        cisoperatorapiv1.ClusterScanNS,
		"name":             name.SafeConcatName(cisoperatorapiv1.ClusterScanConfigMap, clusterscan.Name),
		"runName":          name.SafeConcatName("security-scan-runner", clusterscan.Name),
		"appName":          "rancher-cis-benchmark",
		"advertiseAddress": cisoperatorapiv1.ClusterScanService,
		"sonobuoyImage":    imageConfig.SonobuoyImage + ":" + imageConfig.SonobuoyImageTag,
		"sonobuoyVersion":  imageConfig.SonobuoyImageTag,
	}
	configcm, err := generateConfigMap(clusterscan, "cisscanConfig.template", "./pkg/securityscan/core/templates/cisscanConfig.template", configdata)
	if err != nil {
		return cmMap, err
	}
	cmMap["configcm"] = configcm

	logrus.Infof("CustomBenchmarkConfigMapName Set: %v %v", clusterscanbenchmark.Spec.CustomBenchmarkConfigMapName, clusterscanbenchmark.Spec.CustomBenchmarkConfigMapNamespace)

	var isCustomBenchmark bool
	customBenchmarkConfigMapData := make(map[string]string)
	if clusterscanbenchmark.Spec.CustomBenchmarkConfigMapName != "" {
		isCustomBenchmark = true
		customcm, err := getCustomBenchmarkConfigMap(clusterscanbenchmark, configmapsClient)
		if err != nil {
			logrus.Infof("Error getting customBenchmarkConfigMapData: %v ", clusterscanbenchmark)
			return cmMap, err
		}
		customBenchmarkConfigMapData = customcm.Data
		logrus.Infof("customBenchmarkConfigMapData: %v ", customBenchmarkConfigMapData)
	}

	plugindata := map[string]interface{}{
		"namespace":                    cisoperatorapiv1.ClusterScanNS,
		"name":                         name.SafeConcatName(cisoperatorapiv1.ClusterScanPluginsConfigMap, clusterscan.Name),
		"runName":                      name.SafeConcatName("security-scan-runner", clusterscan.Name),
		"appName":                      "rancher-cis-benchmark",
		"serviceaccount":               cisoperatorapiv1.ClusterScanSA,
		"securityScanImage":            imageConfig.SecurityScanImage + ":" + imageConfig.SecurityScanImageTag,
		"benchmarkVersion":             clusterscanprofile.Spec.BenchmarkVersion,
		"isCustomBenchmark":            isCustomBenchmark,
		"configDir":                    customBenchmarkBaseDir,
		"customBenchmarkConfigMapName": clusterscanbenchmark.Spec.CustomBenchmarkConfigMapName,
		"customBenchmarkConfigMapData": customBenchmarkConfigMapData,
	}
	plugincm, err := generateConfigMap(clusterscan, "pluginConfig.template", "./pkg/securityscan/core/templates/pluginConfig.template", plugindata)
	if err != nil {
		return cmMap, err
	}
	logrus.Infof("plugincm: %v ", plugincm.Data)
	cmMap["plugincm"] = plugincm

	var skipConfigcm *corev1.ConfigMap
	if clusterscanprofile.Spec.SkipTests != nil && len(clusterscanprofile.Spec.SkipTests) > 0 {
		//create user skip config map as well
		// create the cm
		skipDataBytes, err := getOverrideSkipInfoData(clusterscanprofile.Spec.SkipTests)
		if err != nil {
			return cmMap, err
		}
		skipConfigcm = getConfigMapObject(getOverrideConfigMapName(clusterscan), string(skipDataBytes))
		cmMap["skipConfigcm"] = skipConfigcm
	}

	return cmMap, nil
}

func generateConfigMap(clusterscan *cisoperatorapiv1.ClusterScan, templateName string, templateFile string, data map[string]interface{}) (*corev1.ConfigMap, error) {
	configcm := &corev1.ConfigMap{}

	obj, err := parseTemplate(clusterscan, templateName, templateFile, data)
	if err != nil {
		logrus.Infof("Error parsing plugincm: %v ", err)
		return nil, err
	}

	if err := obj.Decode(&configcm); err != nil {
		logrus.Infof("Error decoding plugincm: %v ", err)
		return nil, err
	}
	return configcm, nil
}

func parseTemplate(clusterscan *cisoperatorapiv1.ClusterScan, templateName string, templateFile string, data map[string]interface{}) (*k8Yaml.YAMLOrJSONDecoder, error) {
	cmTemplate, err := template.New(templateName).ParseFiles(templateFile)
	if err != nil {
		return nil, err
	}

	var b bytes.Buffer
	err = cmTemplate.Execute(&b, data)
	if err != nil {
		return nil, err
	}

	return k8Yaml.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(b.String())), 1000), nil
}

func getOverrideConfigMapName(cs *cisoperatorapiv1.ClusterScan) string {
	return name.SafeConcatName(cisoperatorapiv1.ClusterScanUserSkipConfigMap, cs.Name)
}

func getOverrideSkipInfoData(skip []string) ([]byte, error) {
	s := OverrideSkipInfoData{Skip: map[string][]string{CurrentBenchmarkKey: skip}}
	return json.Marshal(s)
}

func getConfigMapObject(cmName, data string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: cisoperatorapiv1.ClusterScanNS,
		},
		Data: map[string]string{
			ConfigFileName: data,
		},
	}
}

func getCustomBenchmarkConfigMap(benchmark *cisoperatorapiv1.ClusterScanBenchmark, configmapsClient wcorev1.ConfigMapController) (*corev1.ConfigMap, error) {
	if benchmark.Spec.CustomBenchmarkConfigMapName == "" {
		return nil, nil
	}
	logrus.Infof("benchmark.Spec.CustomBenchmarkConfigMapNamespace %v", benchmark.Spec.CustomBenchmarkConfigMapNamespace)
	logrus.Infof("benchmark.Spec.CustomBenchmarkConfigMapName %v", benchmark.Spec.CustomBenchmarkConfigMapName)

	return configmapsClient.Get(benchmark.Spec.CustomBenchmarkConfigMapNamespace, benchmark.Spec.CustomBenchmarkConfigMapName, metav1.GetOptions{})
}
