package configuration

import (
	"context"
	"fmt"
	v12 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/ghodss/yaml"
	v1 "github.com/jeremyary/observability-operator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type RuleInfo struct {
	Id          string
	Name        string
	Url         string
	AccessToken string
}

func getUniqueRules(indexes []v1.RepositoryIndex) []RuleInfo {
	var result []RuleInfo
	for _, index := range indexes {
		if index.Config == nil || index.Config.Prometheus == nil {
			continue
		}
		for _, rule := range index.Config.Prometheus.Rules {
			name := getNameFromUrl(rule)
			for _, existing := range result {
				if existing.Name == name {
					continue
				}
			}
			result = append(result, RuleInfo{
				Id:          index.Id,
				Name:        name,
				Url:         fmt.Sprintf("%s/%s", index.BaseUrl, rule),
				AccessToken: index.AccessToken,
			})
		}
	}
	return result
}

func (r *Reconciler) deleteUnrequestedRules(cr *v1.Observability, ctx context.Context, rules []RuleInfo) error {
	// List existing dashboards
	existingRules := &v12.PrometheusRuleList{}
	opts := &client.ListOptions{
		Namespace: cr.Namespace,
	}
	err := r.client.List(ctx, existingRules, opts)
	if err != nil {
		return err
	}

	isRequested := func(name string) bool {
		for _, rule := range rules {
			if name == rule.Name {
				return true
			}
		}
		return false
	}

	// Check which rules are no longer requested and
	// delete them
	for _, rule := range existingRules.Items {
		if isRequested(rule.Name) == false {
			err = r.client.Delete(ctx, rule)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *Reconciler) createRequestedRules(cr *v1.Observability, ctx context.Context, rules []RuleInfo) error {
	// Sync requested prometheus rules
	for _, rule := range rules {
		bytes, err := r.fetchResource(rule.Url, rule.AccessToken)
		if err != nil {
			return err
		}

		parsedRule, err := parseRuleFromYaml(cr, rule.Name, bytes)
		if err != nil {
			return err
		}

		_, err = controllerutil.CreateOrUpdate(ctx, r.client, parsedRule, func() error {
			// Inject managed labels
			injectIdLabel(parsedRule, rule.Id)
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func injectIdLabel(rule *v12.PrometheusRule, id string) {
	for i := 0; i < len(rule.Spec.Groups); i++ {
		for j := 0; j < len(rule.Spec.Groups[i].Rules); j++ {
			rule.Spec.Groups[i].Rules[j].Labels[PrometheusRuleIdentifierKey] = id
		}
	}
}

func parseRuleFromYaml(cr *v1.Observability, name string, source []byte) (*v12.PrometheusRule, error) {
	rule := &v12.PrometheusRule{}
	err := yaml.Unmarshal(source, rule)
	if err != nil {
		return nil, err
	}
	rule.Namespace = cr.Namespace
	rule.Name = name
	return rule, nil
}