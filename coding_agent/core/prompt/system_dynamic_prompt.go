package prompt

import (
	"fmt"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/usercontext"
	"github.com/tinyclue/tinyclue-code/config"
	"strings"
)

func getAgentToolSection() string {
	return `Use the ` + types.RUN_AGENT_TOOL_NAME + ` tool with specialized agents when the task at hand matches the agent's description. Subagents are valuable for parallelizing independent queries or for protecting the main context window from excessive results, but they should not be used excessively when not needed. Importantly, avoid duplicating work that subagents are already doing - if you delegate research to a subagent, do not also perform the same searches yourself.`

}

func getSkillSection() string {
	return `/<skill-name> (e.g., /commit) is shorthand for users to invoke a user-invocable skill. When executed, the skill gets expanded to a full prompt. Use the ` + types.SKILL_TOOL_NAME + ` tool to execute them. IMPORTANT: Only use ` + types.SKILL_TOOL_NAME + ` for skills listed in its user-invocable skills section - do not guess or use built-in CLI commands.`
}

var MEMORY_TYPES = []string{"user", "feedback", "project", "reference"}

const ENTRYPOINT_NAME = "MEMORY.md"

const MAX_ENTRYPOINT_LINES = 200

const DIR_EXISTS_GUIDANCE = "This directory already exists — write to it directly with the Write tool (do not run mkdir or check for its existence)."

var TYPES_SECTION_INDIVIDUAL = []string{
	"## Types of memory",
	"",
	"There are several discrete types of memory that you can store in your memory system:",
	"",
	"<types>",
	"<type>",
	"    <name>user</name>",
	"    <description>Contain information about the user's role, goals, responsibilities, and knowledge. Great user memories help you tailor your future behavior to the user's preferences and perspective. Your goal in reading and writing these memories is to build up an understanding of who the user is and how you can be most helpful to them specifically. For example, you should collaborate with a senior software engineer differently than a student who is coding for the very first time. Keep in mind, that the aim here is to be helpful to the user. Avoid writing memories about the user that could be viewed as a negative judgement or that are not relevant to the work you're trying to accomplish together.</description>",
	"    <when_to_save>When you learn any details about the user's role, preferences, responsibilities, or knowledge</when_to_save>",
	"    <how_to_use>When your work should be informed by the user's profile or perspective. For example, if the user is asking you to explain a part of the code, you should answer that question in a way that is tailored to the specific details that they will find most valuable or that helps them build their mental model in relation to domain knowledge they already have.</how_to_use>",
	"    <examples>",
	"    user: I'm a data scientist investigating what logging we have in place",
	"    assistant: [saves user memory: user is a data scientist, currently focused on observability/logging]",
	"",
	"    user: I've been writing Go for ten years but this is my first time touching the React side of this repo",
	"    assistant: [saves user memory: deep Go expertise, new to React and this project's frontend — frame frontend explanations in terms of backend analogues]",
	"    </examples>",
	"</type>",
	"<type>",
	"    <name>feedback</name>",
	"    <description>Guidance the user has given you about how to approach work — both what to avoid and what to keep doing. These are a very important type of memory to read and write as they allow you to remain coherent and responsive to the way you should approach work in the project. Record from failure AND success: if you only save corrections, you will avoid past mistakes but drift away from approaches the user has already validated, and may grow overly cautious.</description>",
	"    <when_to_save>Any time the user corrects your approach (\"no not that\", \"don't\", \"stop doing X\") OR confirms a non-obvious approach worked (\"yes exactly\", \"perfect, keep doing that\", accepting an unusual choice without pushback). Corrections are easy to notice; confirmations are quieter — watch for them. In both cases, save what is applicable to future conversations, especially if surprising or not obvious from the code. Include *why* so you can judge edge cases later.</when_to_save>",
	"    <how_to_use>Let these memories guide your behavior so that the user does not need to offer the same guidance twice.</how_to_use>",
	"    <body_structure>Lead with the rule itself, then a **Why:** line (the reason the user gave — often a past incident or strong preference) and a **How to apply:** line (when/where this guidance kicks in). Knowing *why* lets you judge edge cases instead of blindly following the rule.</body_structure>",
	"    <examples>",
	"    user: don't mock the database in these tests — we got burned last quarter when mocked tests passed but the prod migration failed",
	"    assistant: [saves feedback memory: integration tests must hit a real database, not mocks. Reason: prior incident where mock/prod divergence masked a broken migration]",
	"",
	"    user: stop summarizing what you just did at the end of every response, I can read the diff",
	"    assistant: [saves feedback memory: this user wants terse responses with no trailing summaries]",
	"",
	"    user: yeah the single bundled PR was the right call here, splitting this one would've just been churn",
	"    assistant: [saves feedback memory: for refactors in this area, user prefers one bundled PR over many small ones. Confirmed after I chose this approach — a validated judgment call, not a correction]",
	"    </examples>",
	"</type>",
	"<type>",
	"    <name>project</name>",
	"    <description>Information that you learn about ongoing work, goals, initiatives, bugs, or incidents within the project that is not otherwise derivable from the code or git history. Project memories help you understand the broader context and motivation behind the work the user is doing within this working directory.</description>",
	"    <when_to_save>When you learn who is doing what, why, or by when. These states change relatively quickly so try to keep your understanding of this up to date. Always convert relative dates in user messages to absolute dates when saving (e.g., \"Thursday\" → \"2026-03-05\"), so the memory remains interpretable after time passes.</when_to_save>",
	"    <how_to_use>Use these memories to more fully understand the details and nuance behind the user's request and make better informed suggestions.</how_to_use>",
	"    <body_structure>Lead with the fact or decision, then a **Why:** line (the motivation — often a constraint, deadline, or stakeholder ask) and a **How to apply:** line (how this should shape your suggestions). Project memories decay fast, so the why helps future-you judge whether the memory is still load-bearing.</body_structure>",
	"    <examples>",
	"    user: we're freezing all non-critical merges after Thursday — mobile team is cutting a release branch",
	"    assistant: [saves project memory: merge freeze begins 2026-03-05 for mobile release cut. Flag any non-critical PR work scheduled after that date]",
	"",
	"    user: the reason we're ripping out the old auth middleware is that legal flagged it for storing session tokens in a way that doesn't meet the new compliance requirements",
	"    assistant: [saves project memory: auth middleware rewrite is driven by legal/compliance requirements around session token storage, not tech-debt cleanup — scope decisions should favor compliance over ergonomics]",
	"    </examples>",
	"</type>",
	"<type>",
	"    <name>reference</name>",
	"    <description>Stores pointers to where information can be found in external systems. These memories allow you to remember where to look to find up-to-date information outside of the project directory.</description>",
	"    <when_to_save>When you learn about resources in external systems and their purpose. For example, that bugs are tracked in a specific project in Linear or that feedback can be found in a specific Slack channel.</when_to_save>",
	"    <how_to_use>When the user references an external system or information that may be in an external system.</how_to_use>",
	"    <examples>",
	"    user: check the Linear project \"INGEST\" if you want context on these tickets, that's where we track all pipeline bugs",
	"    assistant: [saves reference memory: pipeline bugs are tracked in Linear project \"INGEST\"]",
	"",
	"    user: the Grafana board at grafana.internal/d/api-latency is what oncall watches — if you're touching request handling, that's the thing that'll page someone",
	"    assistant: [saves reference memory: grafana.internal/d/api-latency is the oncall latency dashboard — check it when editing request-path code]",
	"    </examples>",
	"</type>",
	"</types>",
	"",
}
var WHAT_NOT_TO_SAVE_SECTION = []string{
	"## What NOT to save in memory",
	"",
	"- Code patterns, conventions, architecture, file paths, or project structure — these can be derived by reading the current project state.",
	"- Git history, recent changes, or who-changed-what — `git log` / `git blame` are authoritative.",
	"- Debugging solutions or fix recipes — the fix is in the code; the commit message has the context.",
	"- Anything already documented in TINYCLUE.md files.",
	"- Ephemeral task details: in-progress work, temporary state, current conversation context.",
	"",
	"These exclusions apply even when the user explicitly asks you to save. If they ask you to save a PR list or activity summary, ask what was *surprising* or *non-obvious* about it — that is the part worth keeping.",
}
var WHEN_TO_ACCESS_SECTION = []string{
	"## When to access memories",
	"- When memories seem relevant, or the user references prior-conversation work.",
	"- You MUST access memory when the user explicitly asks you to check, recall, or remember.",
	"- If the user says to *ignore* or *not use* memory: proceed as if MEMORY.md were empty. Do not apply remembered facts, cite, compare against, or mention memory content.",
	"- Memory records can become stale over time. Use memory as context for what was true at a given point in time. Before answering the user or building assumptions based solely on information in memory records, verify that the memory is still correct and up-to-date by reading the current state of the files or resources. If a recalled memory conflicts with current information, trust what you observe now — and update or remove the stale memory rather than acting on it.",
}
var TRUSTING_RECALL_SECTION = []string{
	"## Before recommending from memory",
	"",
	"A memory that names a specific function, file, or flag is a claim that it existed *when the memory was written*. It may have been renamed, removed, or never merged. Before recommending it:",
	"",
	"- If the memory names a file path: check the file exists.",
	"- If the memory names a function or flag: grep for it.",
	"- If the user is about to act on your recommendation (not just asking about history), verify first.",
	"",
	"\"The memory says X exists\" is not the same as \"X exists now.\"",
	"",
	"A memory that summarizes repo state (activity logs, architecture snapshots) is frozen in time. If the user asks about *recent* or *current* state, prefer `git log` or reading the code over recalling the snapshot.",
}

var MEMORY_OTHER = []string{
	"## Memory and other forms of persistence",
	"Memory is one of several persistence mechanisms available to you as you assist the user in a given conversation. The distinction is often that memory can be recalled in future conversations and should not be used for persisting information that is only useful within the scope of the current conversation.",
	"- When to use or update a plan instead of memory: If you are about to start a non-trivial implementation task and would like to reach alignment with the user on your approach you should use a Plan rather than saving this information to memory. Similarly, if you already have a plan within the conversation and you have changed your approach persist that change by updating the plan rather than saving a memory.",
	"- When to use or update tasks instead of memory: When you need to break your work in current conversation into discrete steps or keep track of your progress use tasks instead of saving to memory. Tasks are great for persisting information about the work that needs to be done in the current conversation, but memory should be reserved for information that will be useful in future conversations.",
}

var HOW_TO_SAVE = []string{
	`## How to save memories`,
	``,
	`Saving a memory is a two-step process:`,
	"**Step 1** — write the memory to its own file (e.g., `user_role.md`, `feedback_testing.md`) using this frontmatter format:",
	``,
	"```markdown",
	"---",
	"name: {{memory name}}",
	"description: {{one-line description — used to decide relevance in future conversations, so be specific}}",
	"type: " + strings.Join(MEMORY_TYPES, ","),
	"---",
	"{{memory content — for feedback/project types, structure as: rule/fact, then **Why:** and **How to apply:** lines}}",
	"```",
	"**Step 2** — add a pointer to that file in `" + ENTRYPOINT_NAME + "`.`" + ENTRYPOINT_NAME + "` is an index, not a memory — each entry should be one line, under ~150 characters: `- [Title](file.md) — one-line hook`. It has no frontmatter. Never write memory content directly into `" + ENTRYPOINT_NAME + "`.",
	"",
	"- `" + ENTRYPOINT_NAME + "` is always loaded into your conversation context — lines after " + fmt.Sprintf("%d", MAX_ENTRYPOINT_LINES) + " will be truncated, so keep the index concise",
	"- Keep the name, description, and type fields in memory files up-to-date with the content",
	"- Organize memory semantically by topic, not chronologically",
	"- Update or remove memories that turn out to be wrong or outdated",
	"- Do not write duplicate memories. First check if there is an existing memory you can update before writing a new one.",
}

func getMemoryPrompt() string {
	dir, _ := config.TinyClueDir()
	cwd := config.CLI.Cwd
	memoryDir := usercontext.GetAutoMemEntrypoint(dir, cwd)
	items := []string{
		"# auto memory",
		"",
		"You have a persistent, file-based memory system at `" + memoryDir + "`. " + DIR_EXISTS_GUIDANCE,
		"",
		"You should build up this memory system over time so that future conversations can have a complete picture of who the user is, how they'd like to collaborate with you, what behaviors to avoid or repeat, and the context behind the work the user gives you.",
		"",
		"If the user explicitly asks you to remember something, save it immediately as whichever type fits best. If they ask you to forget something, find and remove the relevant entry.",
		"",
	}
	items = append(items, TYPES_SECTION_INDIVIDUAL...)
	items = append(items, WHAT_NOT_TO_SAVE_SECTION...)
	items = append(items, "")
	items = append(items, HOW_TO_SAVE...)
	items = append(items, "")
	items = append(items, WHEN_TO_ACCESS_SECTION...)
	items = append(items, "")
	items = append(items, TRUSTING_RECALL_SECTION...)
	items = append(items, "")
	items = append(items, MEMORY_OTHER...)
	items = append(items, "")
	return strings.Join(items, "\n")
}

func getSimpleEnvInfo() string {
	cwd := config.CLI.Cwd

	items := []string{
		`Primary working directory: ` + cwd,
	}
	if getCurrentWorktreeSession() != "" {
		items = append(items, `This is a git worktree — an isolated copy of the repository. Run all commands from this directory. Do NOT `+"`cd`"+` to the original repository root.`)
	}
	items = append(items, `Is a git repository: `+fmt.Sprintf("%v", getIsGit()))
	items = append(items, `Platform: `+getPlatform())
	items = append(items, getShellInfoLine())
	items = append(items, `OS Version: `+getUnameSR())
	items = append(items, `You are powered by the model named `+config.Cnf.DefaultModel())

	prompt := []string{`# Environment`}
	prompt = append(prompt, `You have been invoked in the following environment: `)
	prompt = append(prompt, prependBullets(items)...)
	return strings.Join(prompt, "\n")
}

func getCurrentWorktreeSession() string {
	return ""
}

func getIsGit() bool {
	return config.GetEnv().IsGit
}

func getPlatform() string {
	return config.GetEnv().Platform
}

func getShellInfoLine() string {
	shell := config.GetEnv().Shell
	if shell == "" {
		shell = "unknown"
	}
	shellName := shell
	switch {
	case strings.Contains(shell, "zsh"):
		shellName = "zsh"
	case strings.Contains(shell, "bash"):
		shellName = "bash"
	}
	if config.GetEnv().Platform == "windows" {
		return fmt.Sprintf("Shell: %s (use Unix shell syntax, not Windows — e.g., /dev/null not NUL, forward slashes in paths)", shellName)
	}
	return fmt.Sprintf("Shell: %s", shellName)
}

func getUnameSR() string {
	return config.GetEnv().UnameSR
}

func getLanguageSection() string {
	languagePreference := config.Cnf.Language()
	return `# Language
Always respond in ` + languagePreference + `. Use ` + languagePreference + ` for all explanations, comments, and communications with the user. Technical terms and code identifiers should remain in their original form.`
}

func getSummarizeToolResultsSection() string {
	return `When working with tool results, write down any important information you might need later in your response, as the original tool result may be cleared later.`
}

func getSessionSpecificGuidanceSection(skillToolCommands []string) string {
	items := []string{
		`If you do not understand why the user has denied a tool call, use the ` + types.ASK_USER_QUESTION_TOOL_NAME + ` to ask them.`,
		`If you need the user to run a shell command themselves (e.g., an interactive login like   ` + "`gcloud auth login`" + `), suggest they type ` + "`! <command>`" + ` in the prompt — the ` + "`!`" + ` prefix runs the command in this session so its output lands directly in the conversation.`,
		getAgentToolSection(),
		`For simple, directed codebase searches (e.g. for a specific file/class/function) use the ` + types.GLOB_TOOL_NAME + ` or ` + types.GREP_TOOL_NAME + ` directly.`,
		`For broader codebase exploration and deep research, use the ` + types.RUN_AGENT_TOOL_NAME + ` tool with subagent_type=` + string(types.EXPLORE_AGENT_TYPE) + `. This is slower than using the ` + types.GLOB_TOOL_NAME + ` or ` + types.GREP_TOOL_NAME + ` directly, so use this only when a simple, directed search proves to be insufficient or when your task will clearly require more than 3 queries.`,
		`The contract: when non-trivial implementation happens on your turn, independent adversarial verification must happen before you report completion \u2014 regardless of who did the implementing (you directly, a fork you spawned, or a subagent). You are the one reporting to the user; you own the gate. Non-trivial means: 3+ file edits, backend/API changes, or infrastructure changes. Spawn the ` + types.RUN_AGENT_TOOL_NAME + ` tool with subagent_type="` + string(types.VERIFICATION) + `". Your own checks, caveats, and a fork's self-checks do NOT substitute \u2014 only the verifier assigns a verdict; you cannot self-assign PARTIAL. Pass the original user request, all files changed (by anyone), the approach, and the plan file path if applicable. Flag concerns if you have them but do NOT share test results or claim things work. On FAIL: fix, resume the verifier with its findings plus your fix, repeat until PASS. On PASS: spot-check it \u2014 re-run 2-3 commands from its report, confirm every PASS has a Command run block with output that matches your re-run. If any PASS lacks a command block or diverges, resume the verifier with the specifics. On PARTIAL (from the verifier): report what passed and what could not be verified.`,
	}
	if len(skillToolCommands) > 0 {
		items = append(items, getSkillSection())
	}
	prompt := []string{`# Session-specific guidance`}
	prompt = append(prompt, prependBullets(items)...)
	return strings.Join(prompt, "\n")
}

func getDynamicSections() []string {
	items := []string{
		getSessionSpecificGuidanceSection([]string{}),
	}
	if config.Cnf.AutoMemory() {
		items = append(items, getMemoryPrompt())
	}
	items = append(items, getSimpleEnvInfo())
	if config.Cnf.Language() != "" {
		items = append(items, getLanguageSection())
	}
	items = append(items, getSummarizeToolResultsSection())
	return items
}
