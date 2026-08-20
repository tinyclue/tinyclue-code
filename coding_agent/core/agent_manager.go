package core

import (
	"fmt"
	"sync"

	"github.com/tinyclue/tinyclue-code/coding_agent/core/job"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

// AgentManager 管理所有 agent 实例的注册、查找和生命周期追踪。
// 支持区分父 agent 与子 agent，并提供并发安全的访问。
type AgentManager struct {
	mu    sync.RWMutex
	cache map[string]*Agent // agentId → Agent
}

var AMInstance = &AgentManager{
	cache: make(map[string]*Agent),
}

// Register 注册一个 agent 实例到管理器。
func (am *AgentManager) Register(agent *Agent) {
	if agent == nil {
		return
	}
	am.mu.Lock()
	defer am.mu.Unlock()
	am.cache[agent.agentId] = agent
}

// Unregister 从管理器中移除指定 agent。
func (am *AgentManager) Unregister(agentId string) {
	am.mu.Lock()
	defer am.mu.Unlock()
	delete(am.cache, agentId)
}

// GetAgent 通过 agentId 获取 agent 实例。
func (am *AgentManager) GetAgent(agentId string) *Agent {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.cache[agentId]
}

// GetAgentType 返回指定 agent 的类型。
func (am *AgentManager) GetAgentType(agentId string) (core_types.AgentType, error) {
	am.mu.RLock()
	defer am.mu.RUnlock()
	agent, ok := am.cache[agentId]
	if !ok {
		return "", fmt.Errorf("agent %s not found", agentId)
	}
	return agent.agentType, nil
}

// IsSubAgent 判断指定 agent 是否为子 agent。
func (am *AgentManager) IsSubAgent(agentId string) (bool, error) {
	am.mu.RLock()
	defer am.mu.RUnlock()
	agent, ok := am.cache[agentId]
	if !ok {
		return false, fmt.Errorf("agent %s not found", agentId)
	}
	return agent.subAgent, nil
}

// ListByType 返回指定类型的所有 agentId 列表。
func (am *AgentManager) ListByType(agentType core_types.AgentType) []string {
	am.mu.RLock()
	defer am.mu.RUnlock()
	var result []string
	for id, agent := range am.cache {
		if agent.agentType == agentType {
			result = append(result, id)
		}
	}
	return result
}

// ListSubAgents 返回所有子 agent 的 agentId 列表。
func (am *AgentManager) ListSubAgents() []string {
	am.mu.RLock()
	defer am.mu.RUnlock()
	var result []string
	for id, agent := range am.cache {
		if agent.subAgent {
			result = append(result, id)
		}
	}
	return result
}

// AbortAll 中止所有正在运行的 agent 和后台任务。
func (am *AgentManager) AbortAll() {
	am.mu.RLock()
	for _, agent := range am.cache {
		agent.Abort()
	}
	am.mu.RUnlock()

	// 后台任务（异步 bash 等）挂在 Job 上，其 Abort func 仅在这里被触发；
	// 子 agent 任务的 Abort 同为 agent.Abort，重复调用幂等无害。
	job.JobInstance.ReadAll(func(tasks map[string]*core_types.JobState) {
		for _, t := range tasks {
			if t.Status == core_types.JobStatusCompleted || t.Status == core_types.JobStatusFailed || t.Status == core_types.JobStatusKilled {
				continue
			}
			if t.Abort != nil {
				t.Abort()
			}
		}
	})
}

// Count 返回当前管理的 agent 总数。
func (am *AgentManager) Count() int {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return len(am.cache)
}
