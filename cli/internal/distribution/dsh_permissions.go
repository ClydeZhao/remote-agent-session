package distribution

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"gopkg.in/yaml.v3"
)

type DSHPermissionInput struct {
	Profile          string
	ProfileDirectory string
	Home             string
	StateDir         string
	ServiceStateDir  string
	Executable       string
	Environment      string
	Operation        string
	EffectiveRoots   []string
}

type DSHPermissionPlan struct {
	ID                       string   `json:"plan_id"`
	Operation                string   `json:"operation"`
	Profile                  string   `json:"profile"`
	ProfileDirectory         string   `json:"profile_directory"`
	Environment              string   `json:"environment"`
	Executable               string   `json:"executable"`
	StateDir                 string   `json:"state_directory"`
	ServiceRegistryDirectory string   `json:"service_registry_directory"`
	Configuration            string   `json:"configuration"`
	SharedScope              string   `json:"shared_scope"`
	Operations               []string `json:"operations"`
	BeforeRoots              []string `json:"before_roots"`
	AfterRoots               []string `json:"after_roots"`
	Changes                  bool     `json:"configuration_changes"`
	OwnershipChanges         bool     `json:"ownership_changes,omitempty"`
	ReloadRequired           bool     `json:"reload_required"`
	Status                   string   `json:"status"`
	owner                    string
	patchDigest              string
	recordDigest             string
	homeDigest               string
	afterPatch               []byte
	record                   dshPermissionRecord
}

type dshPermissionOwner struct {
	Executable  string `json:"executable"`
	Environment string `json:"environment"`
	StateDir    string `json:"state_directory"`
}

type dshPermissionGrant struct {
	Added  bool                          `json:"added_by_remote-agent-session"`
	Owners map[string]dshPermissionOwner `json:"owners"`
}

type dshPermissionRecord struct {
	Schema           int                           `json:"schema"`
	ProfileDirectory string                        `json:"profile_directory"`
	Grants           map[string]dshPermissionGrant `json:"grants"`
}

type dshPermissionTransaction struct {
	Schema       int                 `json:"schema"`
	PlanID       string              `json:"plan_id"`
	BeforePatch  string              `json:"before_patch_sha256"`
	AfterPatch   string              `json:"after_patch_sha256"`
	BeforeRecord string              `json:"before_record_sha256"`
	Roots        []string            `json:"after_roots"`
	Record       dshPermissionRecord `json:"after_record"`
	Plan         DSHPermissionPlan   `json:"plan"`
	HomeDigest   string              `json:"home_patch_sha256"`
}

const dshPermissionRecordName = ".remote-agent-session-permissions.json"
const dshPermissionTransactionName = ".remote-agent-session-permissions-pending.json"

func dshPermissionPlanID(plan DSHPermissionPlan, patchDigest, recordDigest, homeDigest string, record dshPermissionRecord) string {
	plan.ID = ""
	plan.Status = "review_required"
	nextRecord, _ := json.Marshal(record)
	identity := struct {
		Plan                             DSHPermissionPlan
		Patch, Record, Home, AfterRecord string
	}{plan, patchDigest, recordDigest, homeDigest, Digest(nextRecord)}
	encoded, _ := json.Marshal(identity)
	return Digest(encoded)
}

func dshPermissionPath(path string, missing bool) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil || absolute == string(filepath.Separator) {
		return "", errors.New("权限范围必须是明确的独立目录")
	}
	for item := absolute; item != filepath.Dir(item); item = filepath.Dir(item) {
		info, err := os.Lstat(item)
		if os.IsNotExist(err) && missing {
			continue
		}
		if err != nil {
			return "", errors.New("权限目录不可读取，请先确认宿主 profile 和状态目录")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if item == "/tmp" || item == "/var" || item == "/etc" {
				continue
			}
			return "", errors.New("权限目录不能包含符号链接，请使用明确的物理目录")
		}
		if !info.IsDir() {
			return "", errors.New("权限范围必须是目录")
		}
	}
	current := absolute
	var suffix []string
	for {
		if _, err := os.Lstat(current); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = filepath.Dir(current)
	}
	physical, err := filepath.EvalSymlinks(current)
	if err != nil {
		return "", err
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		physical = filepath.Join(physical, suffix[i])
	}
	return physical, nil
}

func readDSHPermissionFile(path string, optional bool) ([]byte, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) && optional {
		return nil, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return nil, errors.New("DSH 权限配置文件不可安全读取，未修改配置")
	}
	if err := regular(path); err != nil {
		return nil, errors.New("DSH 权限配置文件不可安全读取，未修改配置")
	}
	if info.Size() > 1<<20 {
		return nil, errors.New("DSH 权限配置过大，未修改配置")
	}
	return os.ReadFile(path)
}

func yamlField(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func setYAMLField(node *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1] = value
			return
		}
	}
	node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

func parseDSHPermissionPatch(data []byte) (*yaml.Node, *yaml.Node, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte("[]\n")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document, extra yaml.Node
	if decoder.Decode(&document) != nil || decoder.Decode(&extra) != io.EOF || len(document.Content) != 1 || document.Content[0].Kind != yaml.SequenceNode {
		return nil, nil, errors.New("无法识别 DSH profile patch，未修改配置")
	}
	var validate func(*yaml.Node) bool
	validate = func(node *yaml.Node) bool {
		if node.Kind == yaml.AliasNode || node.Anchor != "" {
			return false
		}
		if node.Kind == yaml.MappingNode {
			seen := map[string]bool{}
			for i := 0; i < len(node.Content); i += 2 {
				key := node.Content[i]
				if key.Tag != "!!str" || seen[key.Value] {
					return false
				}
				seen[key.Value] = true
			}
		}
		for _, child := range node.Content {
			if !validate(child) {
				return false
			}
		}
		return true
	}
	if !validate(&document) {
		return nil, nil, errors.New("DSH patch 含有别名或重复字段，无法安全合并")
	}
	var selected *yaml.Node
	for _, entry := range document.Content[0].Content {
		if entry.Kind != yaml.MappingNode {
			return nil, nil, errors.New("未知 DSH patch 结构，未修改配置")
		}
		id := yamlField(entry, "id")
		if id == nil || id.Tag != "!!str" || id.Value != "sandbox-policy" {
			continue
		}
		if name := yamlField(entry, "name"); name != nil && (name.Tag != "!!str" || name.Value != "@deepseek-ai/dsh-sandbox-policy") {
			return nil, nil, errors.New("sandbox-policy 指向未知模块，未修改配置")
		}
		if yamlField(entry, "disabled") != nil {
			return nil, nil, errors.New("sandbox-policy 有启停覆盖，无法确认有效策略")
		}
		config := yamlField(entry, "config")
		if config == nil {
			continue
		}
		if config.Kind != yaml.MappingNode || config.Tag != "!!map" {
			return nil, nil, errors.New("sandbox-policy 配置不是可合并的静态对象")
		}
		if roots := yamlField(config, "additionalWritableRoots"); roots != nil {
			selected = roots
		}
	}
	if selected != nil {
		if selected.Kind != yaml.SequenceNode || selected.Tag != "!!seq" {
			return nil, nil, errors.New("额外写目录不是静态列表，未执行任何配置表达式")
		}
		for _, root := range selected.Content {
			if root.Kind != yaml.ScalarNode || root.Tag != "!!str" || !filepath.IsAbs(root.Value) {
				return nil, nil, errors.New("额外写目录含未知值，未修改配置")
			}
		}
	}
	return &document, selected, nil
}

func rewriteDSHPermissionPatch(data []byte, roots []string) ([]byte, error) {
	document, selected, err := parseDSHPermissionPatch(data)
	if err != nil {
		return nil, err
	}
	next := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, root := range roots {
		next.Content = append(next.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: root})
	}
	if selected != nil {
		*selected = *next
	} else {
		entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		config := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setYAMLField(entry, "id", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "sandbox-policy"})
		setYAMLField(config, "additionalWritableRoots", next)
		setYAMLField(entry, "config", config)
		document.Content[0].Content = append(document.Content[0].Content, entry)
	}
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(document); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func dshPermissionRoots(data []byte) ([]string, error) {
	_, roots, err := parseDSHPermissionPatch(data)
	if err != nil {
		return nil, err
	}
	result := []string{}
	if roots != nil {
		for _, root := range roots.Content {
			result = append(result, filepath.Clean(root.Value))
		}
	}
	return result, nil
}

func PlanDSHPermissions(input DSHPermissionInput) (DSHPermissionPlan, error) {
	var plan DSHPermissionPlan
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`).MatchString(input.Profile) || (input.Operation != "install" && input.Operation != "remove") || input.EffectiveRoots == nil {
		return plan, errors.New("需要确切 profile、操作和原宿主目录策略，不能猜测权限范围")
	}
	home, err := dshPermissionPath(input.Home, false)
	if err != nil {
		return plan, err
	}
	profile, err := dshPermissionPath(filepath.Join(home, "profiles", input.Profile), false)
	if err != nil {
		return plan, err
	}
	actual, err := dshPermissionPath(input.ProfileDirectory, false)
	if err != nil || profile != actual {
		return plan, errors.New("原会话的 profile 与指定目标不一致，未修改配置")
	}
	state, err := dshPermissionPath(input.StateDir, true)
	if err != nil {
		return plan, err
	}
	if input.Operation == "install" {
		service := input.ServiceStateDir
		if service == "" {
			service = os.Getenv("REMOTE_AGENT_SESSION_SERVICE_STATE_DIR")
		}
		if service == "" {
			userHome, e := os.UserHomeDir()
			if e != nil {
				return plan, e
			}
			service = filepath.Join(userHome, ".remote-agent-session-service")
		}
		if err := validateDSHServiceState(state, service); err != nil {
			return plan, err
		}
	}
	if state == home || state == profile || state == filepath.Dir(home) {
		return plan, errors.New("状态权限范围过宽，必须选择独立的 Remote Agent Session 状态目录")
	}
	executable, err := filepath.Abs(input.Executable)
	if err != nil {
		return plan, err
	}
	patchPath := filepath.Join(profile, "cordis.patch.yml")
	patch, err := readDSHPermissionFile(patchPath, true)
	if err != nil {
		return plan, err
	}
	profileRoots, err := dshPermissionRoots(patch)
	if err != nil {
		return plan, err
	}
	homePatch, err := readDSHPermissionFile(filepath.Join(home, "cordis.patch.yml"), true)
	if err != nil {
		return plan, err
	}
	if len(homePatch) > 0 {
		_, homeRoots, e := parseDSHPermissionPatch(homePatch)
		if e != nil {
			return plan, e
		}
		if homeRoots != nil {
			return plan, errors.New("DSH home patch 已覆盖写目录，请先在原宿主统一该配置；不会覆盖更高优先级规则")
		}
	}
	pending, err := readDSHPermissionFile(filepath.Join(profile, dshPermissionTransactionName), true)
	if err != nil {
		return plan, err
	}
	if len(pending) > 0 {
		var transaction dshPermissionTransaction
		if json.Unmarshal(pending, &transaction) != nil || transaction.Schema != 1 || transaction.PlanID == "" || transaction.Plan.ID != transaction.PlanID || transaction.Plan.StateDir != state || transaction.Plan.Executable != executable || transaction.Plan.ProfileDirectory != profile || transaction.Plan.Profile != input.Profile || transaction.Plan.Operation != input.Operation || transaction.Plan.Environment != input.Environment || transaction.HomeDigest != Digest(homePatch) || dshPermissionPlanID(transaction.Plan, transaction.BeforePatch, transaction.BeforeRecord, transaction.HomeDigest, transaction.Record) != transaction.PlanID || !slices.Equal(transaction.Roots, transaction.Plan.AfterRoots) {
			return plan, errors.New("有未完成的权限计划，请按原环境、profile 和操作恢复，不覆盖其他修改")
		}
		if Digest(patch) != transaction.BeforePatch && Digest(patch) != transaction.AfterPatch {
			return plan, errors.New("中断后 DSH 配置已变化，需人工核对未完成权限计划")
		}
		plan = transaction.Plan
		plan.Status = "recovery_required"
		plan.patchDigest = transaction.BeforePatch
		plan.recordDigest = transaction.BeforeRecord
		plan.homeDigest = transaction.HomeDigest
		plan.record = transaction.Record
		plan.afterPatch = patch
		if Digest(patch) != transaction.AfterPatch {
			plan.afterPatch, err = rewriteDSHPermissionPatch(patch, transaction.Roots)
			if err != nil {
				return DSHPermissionPlan{}, err
			}
		}
		if Digest(plan.afterPatch) != transaction.AfterPatch {
			return DSHPermissionPlan{}, errors.New("无法安全重建原权限计划")
		}
		return plan, nil
	}
	effective := []string{}
	for _, root := range input.EffectiveRoots {
		physical, e := dshPermissionPath(root, false)
		if e != nil {
			return plan, e
		}
		if !slices.Contains(effective, physical) {
			effective = append(effective, physical)
		}
	}
	for _, root := range profileRoots {
		physical, e := dshPermissionPath(root, false)
		if e != nil {
			return plan, e
		}
		if !slices.Contains(effective, physical) {
			return plan, errors.New("原宿主尚未加载当前目录配置或有更高优先级覆盖，请重新加载后再审阅")
		}
	}
	_, profileRootsNode, _ := parseDSHPermissionPatch(patch)
	if profileRootsNode != nil && len(profileRoots) != len(effective) {
		return plan, errors.New("原宿主目录策略与 profile 配置不同，请重新加载后再生成计划；不会覆盖未知组合")
	}
	recordPath := filepath.Join(profile, dshPermissionRecordName)
	recordData, err := readDSHPermissionFile(recordPath, true)
	if err != nil {
		return plan, err
	}
	record := dshPermissionRecord{Schema: 1, ProfileDirectory: profile, Grants: map[string]dshPermissionGrant{}}
	if len(recordData) > 0 && (json.Unmarshal(recordData, &record) != nil || record.Schema != 1 || record.ProfileDirectory != profile || record.Grants == nil) {
		return plan, errors.New("Remote Agent Session 权限归属记录无效，未覆盖配置")
	}
	owner := Digest([]byte(executable + "\x00" + state))
	for key, grant := range record.Grants {
		if !filepath.IsAbs(key) || grant.Owners == nil {
			return plan, errors.New("Remote Agent Session 权限归属记录无效")
		}
		for id, item := range grant.Owners {
			if id != Digest([]byte(item.Executable+"\x00"+item.StateDir)) || item.StateDir != key {
				return plan, errors.New("Remote Agent Session 权限归属记录不匹配")
			}
		}
	}
	beforeOwnership, _ := json.Marshal(record)
	after := append([]string{}, effective...)
	grant, exists := record.Grants[state]
	if input.Operation == "install" && exists && !slices.Contains(effective, state) {
		return plan, errors.New("原宿主尚未加载已有 Remote Agent Session 授权，请重载 profile 后再审阅，不重复登记")
	}
	if input.Operation == "install" {
		if !exists {
			grant = dshPermissionGrant{Added: !slices.Contains(effective, state), Owners: map[string]dshPermissionOwner{}}
		}
		grant.Owners[owner] = dshPermissionOwner{Executable: executable, Environment: input.Environment, StateDir: state}
		record.Grants[state] = grant
		if !slices.Contains(after, state) {
			after = append(after, state)
		}
	} else if exists {
		delete(grant.Owners, owner)
		if len(grant.Owners) == 0 {
			delete(record.Grants, state)
			if grant.Added {
				after = slices.DeleteFunc(after, func(root string) bool { return root == state })
			}
		} else {
			record.Grants[state] = grant
		}
	}
	changes := !slices.Equal(effective, after)
	afterPatch := patch
	if changes {
		afterPatch, err = rewriteDSHPermissionPatch(patch, after)
		if err != nil {
			return plan, err
		}
	}
	registryRoot, registryErr := serviceRegistryRoot()
	if registryErr != nil {
		return plan, registryErr
	}
	plan = DSHPermissionPlan{ServiceRegistryDirectory: registryRoot, Operation: input.Operation, Profile: input.Profile, ProfileDirectory: profile, Environment: input.Environment, Executable: executable, StateDir: state, Configuration: patchPath, SharedScope: "该 profile 的所有 workspace-write 会话和受限工具共享此目录写访问；不改变沙箱模式或实际任务的单次审批", Operations: []string{"读取", "状态锁创建与读写", "私有临时文件写入与同步", "原子重命名替换", "自身临时文件清理", "登记 CLI 路径保护元数据并协调锁；不授予该目录给宿主"}, BeforeRoots: effective, AfterRoots: after, Changes: changes, ReloadRequired: changes, Status: "review_required", owner: owner, patchDigest: Digest(patch), recordDigest: Digest(recordData), afterPatch: afterPatch, record: record}
	plan.homeDigest = Digest(homePatch)
	afterOwnership, _ := json.Marshal(record)
	plan.OwnershipChanges = !bytes.Equal(beforeOwnership, afterOwnership)
	if input.Operation == "install" {
		roots, err := readDSHRootRegistry(registryRoot, "client-roots.json")
		if err != nil {
			return plan, err
		}
		plan.OwnershipChanges = plan.OwnershipChanges || !slices.Contains(roots, state)
	}
	if !plan.Changes && !plan.OwnershipChanges {
		plan.Status = "already_configured"
		if input.Operation == "remove" {
			plan.Status = "already_removed"
		}
	}
	plan.ID = dshPermissionPlanID(plan, plan.patchDigest, plan.recordDigest, plan.homeDigest, plan.record)
	return plan, nil
}

func dshPermissionLock(profile string) (func(), error) {
	path := filepath.Join(profile, "package.json.lock")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		data, e := readDSHPermissionFile(path, false)
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if e != nil || parseErr != nil || pid <= 0 {
			return nil, errors.New("DSH profile 正在修改或锁无法确认，请稍后重试")
		}
		process, e := os.FindProcess(pid)
		if e != nil || !errors.Is(process.Signal(syscall.Signal(0)), syscall.ESRCH) {
			return nil, errors.New("DSH profile 正在修改，请稍后重试")
		}
		if e = os.Remove(path); e != nil {
			return nil, e
		}
		file, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	}
	if err != nil {
		return nil, errors.New("无法取得 DSH profile 写锁，未修改配置")
	}
	if _, err = fmt.Fprintf(file, "%d\n", os.Getpid()); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
		return nil, err
	}
	return func() { os.Remove(path) }, nil
}

func ApplyDSHPermissions(plan DSHPermissionPlan, approved string) error {
	if approved == "" || approved != plan.ID {
		return errors.New("请先审阅权限计划，再使用其 --apply plan-id 明确应用")
	}
	if plan.Operation == "install" {
		root, err := serviceRegistryRoot()
		if err != nil {
			return err
		}
		release, err := serviceRegistryLock(root)
		if err != nil {
			return err
		}
		defer release()
		if err := validateDSHServiceState(plan.StateDir, os.Getenv("REMOTE_AGENT_SESSION_SERVICE_STATE_DIR")); err != nil {
			return err
		}
		if err := registerDSHClientState(root, plan.StateDir); err != nil {
			return err
		}
	}
	profile, err := dshPermissionPath(plan.ProfileDirectory, false)
	if err != nil {
		return err
	}
	unlock, err := dshPermissionLock(profile)
	if err != nil {
		return err
	}
	defer unlock()
	return applyDSHPermissionFiles(plan, atomic)
}

func applyDSHPermissionFiles(plan DSHPermissionPlan, write func(string, []byte, os.FileMode) error) error {
	profile := plan.ProfileDirectory
	pendingPath := filepath.Join(profile, dshPermissionTransactionName)
	pending, err := readDSHPermissionFile(pendingPath, true)
	if err != nil {
		return err
	}
	patch, err := readDSHPermissionFile(plan.Configuration, true)
	if err != nil {
		return err
	}
	recordPath := filepath.Join(profile, dshPermissionRecordName)
	record, err := readDSHPermissionFile(recordPath, true)
	if err != nil {
		return err
	}
	nextRecord, err := json.Marshal(plan.record)
	if err != nil {
		return err
	}
	homePatch, err := readDSHPermissionFile(filepath.Join(filepath.Dir(filepath.Dir(profile)), "cordis.patch.yml"), true)
	if err != nil {
		return err
	}
	if Digest(homePatch) != plan.homeDigest {
		return errors.New("DSH home 配置在审阅后已变化，请重新生成权限计划")
	}
	transaction := dshPermissionTransaction{Schema: 1, PlanID: plan.ID, BeforePatch: plan.patchDigest, AfterPatch: Digest(plan.afterPatch), BeforeRecord: plan.recordDigest, Roots: plan.AfterRoots, Record: plan.record, Plan: plan, HomeDigest: Digest(homePatch)}
	if len(pending) > 0 {
		if json.Unmarshal(pending, &transaction) != nil || transaction.Schema != 1 || transaction.PlanID != plan.ID || transaction.Record.ProfileDirectory != profile {
			return errors.New("有未完成的 Remote Agent Session 权限变更，请用原计划恢复，不覆盖其他修改")
		}
		if Digest(patch) != transaction.BeforePatch && Digest(patch) != transaction.AfterPatch {
			return errors.New("权限变更中断后 DSH 配置已变化，请人工核对原计划，未覆盖")
		}
		if Digest(record) != transaction.BeforeRecord && Digest(record) != Digest(nextRecord) {
			return errors.New("权限归属在恢复期间已变化，未覆盖")
		}
	} else {
		if Digest(patch) != plan.patchDigest || Digest(record) != plan.recordDigest {
			return errors.New("权限计划已过期，请重新生成并审阅差异")
		}
		if plan.Status == "already_configured" || plan.Status == "already_removed" {
			return nil
		}
		data, _ := json.Marshal(transaction)
		if err = write(pendingPath, data, 0600); err != nil {
			return err
		}
	}
	if plan.Operation == "install" {
		if err := os.MkdirAll(plan.StateDir, 0700); err != nil {
			return errors.New("无法准备已审阅的独立状态目录，配置尚未应用")
		}
		if _, err := dshPermissionPath(plan.StateDir, false); err != nil {
			return err
		}
	}
	if Digest(patch) != transaction.AfterPatch {
		if err = write(plan.Configuration, plan.afterPatch, 0600); err != nil {
			return errors.New("DSH 权限配置保存未完成，请保留原计划并重试")
		}
	}
	if err = write(recordPath, nextRecord, 0600); err != nil {
		return errors.New("权限归属保存未完成，请使用原计划恢复；不要重复扩大权限")
	}
	if err = os.Remove(pendingPath); err != nil {
		return errors.New("权限配置已保存，但恢复记录清理未完成，请使用原计划重试")
	}
	return nil
}

func separateDSHState(state, service string) error {
	absolute, err := filepath.Abs(service)
	if err != nil {
		return err
	}
	current := absolute
	var suffix []string
	for {
		if _, err := os.Lstat(current); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return err
		}
		suffix = append(suffix, filepath.Base(current))
		current = filepath.Dir(current)
	}
	physical, err := filepath.EvalSymlinks(current)
	if err != nil {
		return err
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		physical = filepath.Join(physical, suffix[i])
	}
	contains := func(root, path string) bool {
		relative, e := filepath.Rel(root, path)
		return e == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
	}
	if contains(state, physical) || contains(physical, state) {
		return errors.New("CLI 状态目录与服务私有目录重叠；请使用独立 CLI 目录，不能向 DSH 授予飞书凭据、加密密钥或数据库访问")
	}
	for path := state; path != filepath.Dir(path); path = filepath.Dir(path) {
		for _, name := range []string{"feishu.env", "encryption.key", "postgres/PG_VERSION"} {
			if _, err := os.Lstat(filepath.Join(path, name)); err == nil {
				return errors.New("该目录包含或位于服务私有状态中，不能作为 DSH CLI 写根；请使用独立 CLI 状态目录")
			}
		}
	}
	return nil
}

func serviceRegistryRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".remote-agent-session-runtime"), nil
}

func validateDSHServiceState(state, service string) error {
	root, err := serviceRegistryRoot()
	if err != nil {
		return err
	}
	if service == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		service = filepath.Join(home, ".remote-agent-session-service")
	}
	if err := separateDSHState(state, service); err != nil {
		return err
	}
	if err := separateDSHState(state, root); err != nil {
		return err
	}
	roots, err := readDSHRootRegistry(root, "service-roots.json")
	if err != nil {
		return err
	}
	if _, err := readDSHRootRegistry(root, "client-roots.json"); err != nil {
		return err
	}
	for _, known := range roots {
		if err := separateDSHState(state, known); err != nil {
			return err
		}
	}
	return nil
}

func readDSHRootRegistry(root, name string) ([]string, error) {
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !serviceRegistryOwned(info) {
		return nil, errors.New("路径保护元数据目录不安全")
	}
	path := filepath.Join(root, name)
	info, err = os.Lstat(path)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil || !info.Mode().IsRegular() || !serviceRegistryOwned(info) {
		return nil, errors.New("路径保护元数据文件不安全")
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 1<<20 {
		return nil, errors.New("无法读取路径保护元数据")
	}
	var registry struct {
		Schema int      `json:"schema"`
		Roots  []string `json:"roots"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&registry) != nil || registry.Schema != 1 || registry.Roots == nil || len(registry.Roots) > 4096 {
		return nil, errors.New("路径保护元数据损坏")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("路径保护元数据损坏")
	}
	for _, path := range registry.Roots {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) {
			return nil, errors.New("路径保护元数据包含不安全路径")
		}
	}
	return registry.Roots, nil
}

func registerDSHClientState(root, state string) error {
	roots, err := readDSHRootRegistry(root, "client-roots.json")
	if err != nil {
		return err
	}
	if slices.Contains(roots, state) {
		return nil
	}
	if len(roots) >= 4096 {
		return errors.New("CLI 路径保护记录已满，请核对历史安装")
	}
	roots = append(roots, state)
	slices.Sort(roots)
	data, err := json.Marshal(struct {
		Schema int      `json:"schema"`
		Roots  []string `json:"roots"`
	}{1, roots})
	if err != nil {
		return err
	}
	return atomic(filepath.Join(root, "client-roots.json"), data, 0600)
}
