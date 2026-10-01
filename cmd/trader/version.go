package main

import "runtime/debug"

// buildVersion은 바이너리에 박힌 git 커밋(+dirty 표시)을 돌려줍니다. VM은 서비스 시작 시점의
// 바이너리를 쓰므로, 같은 날 섞인 버전을 로그/스냅샷만으로 구분할 수 있어야 합니다.
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "unknown"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-dirty"
	}
	return rev
}
