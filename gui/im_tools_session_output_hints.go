package main

import (
	"fmt"
	"strings"
)

func appendNoOutputSessionHint(b *strings.Builder, sessionID string, facts sessionOutputHintFacts) {
	if facts.Status.IsRunning() {
		b.WriteString(fmt.Sprintf("\n📌 会话已就绪但暂无输出——编程工具在等待输入。请立即调用 send_and_observe(session_id=%q, text=\"编程指令\") 发送任务。", sessionID))
	} else if facts.Status.IsStarting() {
		b.WriteString("\n⏳ 会话正在启动中，请稍后再次调用 get_session_output 检查状态（最多再检查 1 次）。")
	}
}

func appendBusySessionHint(b *strings.Builder, facts sessionOutputHintFacts) {
	if !facts.Status.IsBusy() {
		return
	}
	if facts.HasAPIRetry {
		b.WriteString("\n⏳ 上游 API 限流/暂时不可用，编程工具正在自动重试中。这是正常现象，请耐心等待（可能需要 30-60 秒）。")
		b.WriteString("\n⚠️ 不要放弃或创建新会话——API 重试通常会成功。稍后再调用 get_session_output 检查进度。")
		return
	}
	switch facts.StallState {
	case StallStateSuspected:
		b.WriteString("\n⏳ 编程工具输出暂停，系统正在尝试恢复，请稍后再检查")
	case StallStateStuck:
		b.WriteString("\n⚠️ 编程工具可能已卡住，建议发送具体指令或终止会话")
	default:
		b.WriteString("\n⏳ 编程工具正在工作中，请等待后再检查进度")
	}
}

func appendWaitingInputSessionHint(b *strings.Builder, sessionID string, facts sessionOutputHintFacts) {
	if !facts.Status.IsWaitingInput() {
		return
	}
	if facts.HasRecentTransientAPI && facts.CompletionLevel != CompletionCompleted {
		b.WriteString("\n⚠️ 编程工具遇到 API 错误后恢复，任务可能未完成。")
		b.WriteString(fmt.Sprintf("\n📌 请重新发送指令让编程工具继续工作：send_and_observe(session_id=%q, text=\"继续完成之前的任务\")", sessionID))
		b.WriteString("\n不要放弃——API 错误是暂时的，重试通常会成功。")
		return
	}
	switch facts.CompletionLevel {
	case CompletionCompleted:
		b.WriteString("\n✅ 任务似乎已完成，可以查看结果")
	case CompletionIncomplete:
		if facts.StructuredSession {
			if facts.AutoContinueCount >= 10 {
				b.WriteString("\n⚠️ 已自动续接 10 次，建议告知用户当前进度并询问是否继续。")
			} else {
				b.WriteString("\n🔄 编程工具因 token/turn 限制暂停，任务未完成。")
				b.WriteString(fmt.Sprintf("\n📌 立即调用 send_and_observe(session_id=%q, text=\"继续完成之前的任务\") 让编程工具继续工作。", sessionID))
				b.WriteString("\n⚠️ 不要询问用户是否继续——直接发送续接指令。")
			}
		} else {
			b.WriteString("\n⚠️ 任务似乎未完成，建议发送「继续」让编程工具继续工作")
		}
	default:
		if facts.StructuredSession {
			b.WriteString(fmt.Sprintf("\n📌 编程工具已暂停。调用 send_and_observe(session_id=%q, text=\"继续\") 让编程工具继续工作，或查看输出判断任务是否已完成。", sessionID))
		}
	}
}

func appendTerminalSessionExitHint(b *strings.Builder, facts sessionOutputHintFacts) {
	if facts.HasNonZeroTerminalExit() {
		if facts.StructuredSession && *facts.ExitCode == 1 {
			writeAutoResumeHint(b, facts.ResumeContext, "编程工具因 token 耗尽正常退出，但任务可能未完成。")
		} else if facts.StructuredSession && *facts.ExitCode > 1 {
			appendStructuredSessionExitHint(b, facts)
		} else {
			appendPTYSessionExitHint(b, facts)
		}
	}
	if facts.HasStructuredNormalExitWithPossibleUnfinishedWork() {
		writeAutoResumeHint(b, facts.ResumeContext, "编程工具已正常退出（可能达到 max-turns 限制），任务可能未完成。")
	}
}

func appendStructuredSessionExitHint(b *strings.Builder, facts sessionOutputHintFacts) {
	if facts.ExitCode == nil {
		return
	}
	resumeCount := facts.ResumeCount()
	if facts.FatalSessionError {
		b.WriteString(fmt.Sprintf("\n🛑 编程工具遇到不可恢复的错误退出（退出码 %d）。", *facts.ExitCode))
		b.WriteString(fmt.Sprintf("\n请立即将错误信息告知用户，并建议检查 %s 的 API Key 配置和安装。", facts.Tool))
		b.WriteString("\n不要重试——这是配置问题，重试不会解决。")
	} else if resumeCount < 3 {
		b.WriteString(fmt.Sprintf("\n⚠️ 编程工具异常退出（退出码 %d），可能是 API 限流、网络波动或临时服务故障。", *facts.ExitCode))
		b.WriteString(fmt.Sprintf("\n📌 自动重试（第 %d/3 次）：立即创建新会话重试，使用相同参数。", resumeCount+1))
		b.WriteString("\n不要询问用户——直接重试。这类错误通常是暂时的，重试大概率会成功。")
	} else {
		b.WriteString(fmt.Sprintf("\n🛑 编程工具已连续失败 %d 次（退出码 %d）。", resumeCount, *facts.ExitCode))
		b.WriteString(fmt.Sprintf("\n请将错误信息告知用户，建议检查 %s 的上游 API 状态或稍后再试。", facts.Tool))
	}
}

func appendPTYSessionExitHint(b *strings.Builder, facts sessionOutputHintFacts) {
	if facts.ExitCode == nil {
		return
	}
	if facts.FatalSessionError {
		b.WriteString(fmt.Sprintf("\n🛑 会话遇到不可恢复的错误退出（退出码 %d）。", *facts.ExitCode))
		b.WriteString(fmt.Sprintf("\n请立即将错误信息告知用户，并建议检查 %s 的安装和配置。", facts.Tool))
	} else {
		b.WriteString(fmt.Sprintf("\n🛑 会话已失败退出（退出码 %d），可能是临时错误。", *facts.ExitCode))
		b.WriteString("\n📌 建议创建新会话重试。如果连续失败，请将错误信息告知用户。")
	}
}
