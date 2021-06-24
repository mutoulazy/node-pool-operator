/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/node/v1beta1"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nodesv1 "node-pool-operator/api/v1"
)

// NodePoolReconciler reconciles a NodePool object
type NodePoolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const nodeFinalizer = "node.finalizers.node-pool.lailin.xyz"

//+kubebuilder:rbac:groups=nodes.lailin.xyz,resources=nodepools,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nodes.lailin.xyz,resources=nodepools/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nodes.lailin.xyz,resources=nodepools/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the NodePool object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *NodePoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// 获取资源
	pool := &nodesv1.NodePool{}
	if err := r.Get(ctx, req.NamespacedName, pool); err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Info("Get NodePool error")
			return ctrl.Result{}, err
		}
		log.Info("Get NodePool NotFound")
		return ctrl.Result{}, nil
	}

	var nodes v1.NodeList

	// 查看是否存在对应的节点，如果存在那么就给这些节点加上数据
	err := r.List(ctx, &nodes, &client.ListOptions{LabelSelector: pool.NodeLabelSelector()})
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}

	// 删除时间戳不为空 进入预删除逻辑
	if !pool.DeletionTimestamp.IsZero() {
		log.Info("Pre Delete Handler")
		return ctrl.Result{}, r.nodeFinalizer(ctx, pool, nodes.Items)
	}
	// 删除时间戳为空说明现在不需要删除该数据，我们将 nodeFinalizer 加入到资源中
	if !containsString(pool.Finalizers, nodeFinalizer) {
		pool.Finalizers = append(pool.Finalizers, nodeFinalizer)
		if err := r.Update(ctx, pool); err != nil {
			log.Info("Update Finalizers Error")
			return ctrl.Result{}, err
		}
	}

	if len(nodes.Items) > 0 {
		log.Info("find nodes, will merge data", "nodes", len(nodes.Items))
		for _, node := range nodes.Items {
			node := node
			//err := r.Patch(ctx, pool.Spec.ApplyNode(node), client.Merge)
			err := r.Update(ctx, pool.Spec.ApplyNode(node))
			if err != nil {
				log.Info("Update Node Labels Error")
				return ctrl.Result{}, err
			}
		}
	}

	runtimeClass := &v1beta1.RuntimeClass{}
	err = r.Get(ctx, client.ObjectKeyFromObject(pool.RuntimeClass()), runtimeClass)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}

	// 如果不存在创建一个新的RuntimeClass
	if runtimeClass.Name == "" {
		runtimeClass = pool.RuntimeClass()
		err = ctrl.SetControllerReference(pool, runtimeClass, r.Scheme)
		if err != nil {
			log.Info("SetControllerReference error")
			return ctrl.Result{}, err
		}
		err = r.Create(ctx, runtimeClass)
		if err != nil {
			log.Info("Create runtimeClass error")
			return ctrl.Result{}, err
		}
	}

	// RuntimeClass如果存在则更新
	err = r.Patch(ctx, pool.RuntimeClass(), client.Merge)
	if err != nil {
		log.Info("Patch RuntimeClass Error")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodePoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&nodesv1.NodePool{}).
		Complete(r)
}

// 节点预删除逻辑
func (r *NodePoolReconciler) nodeFinalizer(ctx context.Context, pool *nodesv1.NodePool, nodes []v1.Node) error {
	for _, node := range nodes {
		node := node
		// 更新节点和污点信息
		err := r.Update(ctx, pool.Spec.CleanNode(node))
		if err != nil {
			return err
		}
	}

	// 删除预处理完毕, 移除nodeFinalizer
	pool.Finalizers = removeString(pool.Finalizers, nodeFinalizer)
	return r.Update(ctx, pool)
}

// 检查字符串切片是否包含某个字符串
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// 移除字符串切片中对应字符串
func removeString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}
