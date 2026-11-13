package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/api"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/output"
	"github.com/google/uuid"
)

func completePairing(pending api.DeviceCreated) bool {
	return pending.DeviceId != uuid.Nil && pending.ClaimSecret != "" && pending.UserCode != "" && !pending.ExpiresAt.IsZero()
}

func invalidPairing() error {
	return fail("CLI_PAIRING_INVALID", "local_state", "本机配对记录无法使用，已保留。请让 Agent 在不打印秘密的情况下核对并修复该记录，再重试。", "user_action", 3)
}

func startLogin(r *remote, name string) (created api.DeviceCreated, err error) {
	data, e := r.store.Pairing(r.config.Server)
	if e == nil {
		if json.Unmarshal(data, &created) != nil || !completePairing(created) {
			return created, invalidPairing()
		}
		if time.Now().Before(created.ExpiresAt) {
			return created, nil
		}
	}
	if name == "" {
		name, err = deviceName()
		if err != nil {
			return created, err
		}
	}
	resp, e := r.client.StartV1AuthDevicesPost(context.Background(), api.DeviceStart{DeviceName: name})
	if err = decode(resp, e, &created, "user_action"); err != nil {
		return
	}
	if !completePairing(created) {
		err = fail("CLI_RESPONSE", "protocol", "配对响应不完整。", "none", exitRemote)
		return
	}
	data, _ = json.Marshal(created)
	err = r.store.SavePairing(r.config.Server, data)
	return
}

func finishLogin(r *remote) (result api.DeviceToken, err error) {
	data, e := r.store.Pairing(r.config.Server)
	if e != nil {
		return result, fail("CLI_PAIRING_REQUIRED", "authentication", "请先运行 remote-agent-session login。", "user_action", exitAuth)
	}
	var pending api.DeviceCreated
	if json.Unmarshal(data, &pending) != nil || !completePairing(pending) {
		return result, invalidPairing()
	}
	resp, e := r.client.ClaimV1AuthDevicesDeviceIdTokenPost(context.Background(), pending.DeviceId, api.DeviceClaim{ClaimSecret: &pending.ClaimSecret})
	if err = decode(resp, e, &result, "same_request"); err != nil {
		return
	}
	if result.State == api.Expired {
		return result, fail("CLI_PAIRING_EXPIRED", "authentication", "配对码已过期，请重新运行 remote-agent-session setup 或 login。", "user_action", exitAuth)
	}
	if result.State != api.Authorized {
		if result.State != api.Pending && result.State != api.AwaitingConfirmation {
			err = fail("CLI_RESPONSE", "protocol", "未知的配对状态。", "none", exitRemote)
		}
		return
	}
	if result.AccessToken == nil || *result.AccessToken == "" || (result.TokenType != nil && *result.TokenType != api.Bearer) {
		return result, fail("CLI_RESPONSE", "protocol", "登录响应缺少设备凭据。", "none", exitRemote)
	}
	if err = r.store.SaveCredential(r.config.Server, []byte(*result.AccessToken)); err != nil {
		return
	}
	err = r.store.ClearPairing(r.config.Server)
	return
}

func login(p output.Printer, r *remote, args []string) int {
	if len(args) > 0 && args[0] == "finish" {
		if len(args) != 1 {
			return usage(p)
		}
		result, err := finishLogin(r)
		if err != nil {
			return report(p, err)
		}
		if result.State != api.Authorized {
			if exit := p.Result(map[string]any{"state": result.State, "retry_after": 3}, "等待飞书确认；完成后运行 remote-agent-session login finish。"); exit != 0 {
				return exit
			}
			return exitPending
		}
		return p.Result(map[string]any{"state": "authorized", "expires_at": result.ExpiresAt}, "登录成功，设备登录态已保存。")
	}
	f := flags("login")
	name := f.String("device-name", "", "设备名称")
	if f.Parse(args) != nil || f.NArg() != 0 {
		return usage(p)
	}
	created, err := startLogin(r, *name)
	if err != nil {
		return report(p, err)
	}
	return p.Result(map[string]any{"device_id": created.DeviceId, "user_code": created.UserCode, "verification_uri": created.VerificationUri, "expires_at": created.ExpiresAt, "poll_interval": created.PollInterval, "simulated": created.Simulated}, "请向飞书机器人发送配对码："+created.UserCode+"\n机器人入口："+created.VerificationUri+"\n按机器人提示确认后，运行 remote-agent-session login finish。")
}

func logout(p output.Printer, r *remote, args []string) int {
	if len(args) != 0 {
		return usage(p)
	}
	resp, e := r.client.LogoutV1AuthSessionDelete(context.Background())
	if err := decode(resp, e, nil, "same_request"); err != nil {
		return report(p, err)
	}
	if err := r.store.ForgetCredential(r.config.Server); err != nil {
		return report(p, err)
	}
	return p.Result(map[string]string{"state": "logged_out"}, "已退出本设备；远端作业和服务端监控不受影响。")
}
