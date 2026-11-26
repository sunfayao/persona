#!/bin/sh
# Admin access prompts sourced by deploy.sh.

# install_language [locale]: print one of en, zh-Hans or ja. With no argument it reads LC_ALL, then LANG.
install_language() {
	_install_locale=${1:-${LC_ALL:-${LANG:-}}}
	case "$_install_locale" in
	zh* | ZH*) printf '%s' zh-Hans ;;
	ja* | JA*) printf '%s' ja ;;
	*) printf '%s' en ;;
	esac
}

# install_text <key>: print the text for key in INSTALL_LANG. Keys and all three translations live in this block.
# Catalog entries are printed as data: install_format, not this lookup, substitutes their placeholders.
# shellcheck disable=SC2183 # Catalog entries are format strings; install_format supplies their arguments.
install_text() {
	case "$INSTALL_LANG:$1" in
	en:admin_help) cat <<'TEXT'
Who can open the hub admin page (<hub address>/admin, for adding computers, managing devices and changing the network)?
  1) loopback  Only the hub machine itself. Other computers need an SSH tunnel. Safest, least convenient
  2) lan       This machine and the hub's local network (default). Available at home or the office, not from the internet or other networks
  3) cidrs     This machine and the networks you list, such as a VPN network. Public networks in the list still require TLS
  4) any       Anywhere through the TLS port (the local network still works). Exposed to the internet and protected only by the admin password and login rate limiting; without TLS this is the same as lan
Change it later on the hub with personad hub admin access <scope>. The web page can narrow access, but cannot widen it.
TEXT
		;;
	zh-Hans:admin_help) cat <<'TEXT'
hub 管理界面（<hub 地址>/admin：加电脑、管设备、改网络）谁能打开？
  1) loopback  只有 hub 这台机器自己，别的电脑要用 ssh 隧道。最安全，用着最麻烦
  2) lan       本机 + 和 hub 同一网段的局域网（默认）。家里 / 办公室的局域网里都能打开，公网和别的网段打不开
  3) cidrs     本机 + 你列出的网段，例如 VPN 的网段；其中的公网网段也只能经 TLS
  4) any       任何地方经 TLS 端口都能打开（局域网照旧）。暴露在公网上，只靠管理员密码和登录限流挡着；没开 TLS 时等于 lan
以后在 hub 机器上用 personad hub admin access <范围> 改；网页里只能收窄，不能放宽。
TEXT
		;;
	ja:admin_help) cat <<'TEXT'
hub 管理画面（<hub のアドレス>/admin：コンピュータ、端末、ネットワークの管理）を誰が開けますか？
  1) loopback  hub のコンピュータだけ。ほかのコンピュータには SSH トンネルが必要です。最も安全ですが不便です
  2) lan       このコンピュータと hub と同じ LAN（既定）。自宅や職場の LAN から開けますが、インターネットや別のネットワークからは開けません
  3) cidrs     このコンピュータと、VPN など指定したネットワーク。指定した公開ネットワークでも TLS が必要です
  4) any       TLS ポートを通してどこからでも開けます（LAN も利用可能）。インターネットに公開され、管理者パスワードとログイン制限だけで保護されます。TLS がなければ lan と同じです
後で hub 上の personad hub admin access <範囲> で変更できます。Web 画面では範囲を狭めることだけができます。
TEXT
		;;
	en:admin_choose) printf '%s' 'Choose one [1-4, Enter = 2 lan] ' ;;
	zh-Hans:admin_choose) printf '%s' '选哪个？[1-4，回车 = 2 lan] ' ;;
	ja:admin_choose) printf '%s' '選んでください [1-4、Enter = 2 lan] ' ;;
	en:admin_invalid_choice) printf '%s' 'Enter a number from 1 to 4' ;;
	zh-Hans:admin_invalid_choice) printf '%s' '请输入 1 到 4' ;;
	ja:admin_invalid_choice) printf '%s' '1 から 4 を入力してください' ;;
	en:admin_cidrs) printf '%s' 'Networks, separated by commas (for example 100.64.0.0/10,192.0.2.0/24): ' ;;
	zh-Hans:admin_cidrs) printf '%s' '网段，逗号分隔（例如 100.64.0.0/10,192.0.2.0/24）：' ;;
	ja:admin_cidrs) printf '%s' 'ネットワークをカンマで区切って入力してください（例：100.64.0.0/10,192.0.2.0/24）：' ;;
	en:admin_any_confirm) printf '%s' 'The admin page will be open to the internet. Enter any to confirm; anything else uses lan: ' ;;
	zh-Hans:admin_any_confirm) printf '%s' '管理界面会对公网开放。输入 any 确认，别的改用 lan：' ;;
	ja:admin_any_confirm) printf '%s' '管理画面がインターネットに公開されます。確認するには any、それ以外なら lan を使用します：' ;;
	en:admin_any_fallback) printf '%s' 'Not confirmed; using lan' ;;
	zh-Hans:admin_any_fallback) printf '%s' '没有确认，改用 lan' ;;
	ja:admin_any_fallback) printf '%s' '確認されなかったため lan を使用します' ;;
	en:admin_reminder) printf '%s' 'Reminder: the admin page (/admin) access was not chosen; using lan (the hub machine and its local network). Change it on the hub with personad hub admin access <loopback|lan|cidrs|any>' ;;
	zh-Hans:admin_reminder) printf '%s' '提醒：hub 管理界面（/admin）的访问范围没有选，用默认的 lan（hub 本机和同一局域网）；要改在 hub 机器上运行 personad hub admin access <loopback|lan|cidrs|any>' ;;
	ja:admin_reminder) printf '%s' 'お知らせ：管理画面（/admin）のアクセス範囲が未選択のため、lan（hub と同じ LAN）を使用します。変更するには hub で personad hub admin access <loopback|lan|cidrs|any> を実行してください' ;;
	en:admin_access_invalid) printf '%s' 'personad: --admin-access must be loopback, lan, cidrs (with --admin-cidrs) or any' ;;
	zh-Hans:admin_access_invalid) printf '%s' 'personad：--admin-access 必须是 loopback、lan、cidrs（并提供 --admin-cidrs）或 any' ;;
	ja:admin_access_invalid) printf '%s' 'personad：--admin-access は loopback、lan、cidrs（--admin-cidrs が必要）、any のいずれかです' ;;
	*) return 1 ;;
	esac
}

# Localized output to stderr and the terminal.
install_format() {
	_install_key=$1
	shift
	_install_format=$(install_text "$_install_key") || {
		printf 'personad: missing deploy text %s/%s\n' "$INSTALL_LANG" "$_install_key" >&2
		return 70
	}
	# The format string is trusted repository text; dynamic values are always separate arguments.
	printf "$_install_format" "$@"
}

warn() {
	install_format "$@" >&2
	printf '\n' >&2
}

prompt() {
	install_format "$@" >/dev/tty
}

admin_access_help() {
	install_text admin_help
	printf '\n'
}

# admin_access_valid <access> <cidrs>: the scope is one of the four, and the networks look like networks.
admin_access_valid() {
	case "$1" in loopback | lan | any) [ -z "$2" ] ;; cidrs) [ -n "$2" ] && case "$2" in *[!0-9A-Fa-f.:/,]*) false ;; *) true ;; esac ;; *) false ;; esac
}

# ask_admin_access: ADMIN_ACCESS (and ADMIN_CIDRS) from the terminal; returns 1 when there is none.
ask_admin_access() {
	(: </dev/tty) 2>/dev/null || return 1
	admin_access_help >/dev/tty
	while :; do
		prompt admin_choose
		read -r answer </dev/tty || return 1
		case "$answer" in
		'' | 2 | lan) ADMIN_ACCESS=lan ;;
		1 | loopback) ADMIN_ACCESS=loopback ;;
		3 | cidrs) ADMIN_ACCESS=cidrs ;;
		4 | any) ADMIN_ACCESS=any ;;
		*) install_text admin_invalid_choice >/dev/tty && printf '\n' >/dev/tty && continue ;;
		esac
		break
	done
	ADMIN_CIDRS=
	while [ "$ADMIN_ACCESS" = cidrs ] && ! admin_access_valid cidrs "$ADMIN_CIDRS"; do
		prompt admin_cidrs
		read -r ADMIN_CIDRS </dev/tty || return 1
		ADMIN_CIDRS=$(printf '%s' "$ADMIN_CIDRS" | tr -d ' ')
	done
	if [ "$ADMIN_ACCESS" = any ]; then
		prompt admin_any_confirm
		read -r confirm </dev/tty || return 1
		[ "$confirm" = any ] || { install_text admin_any_fallback >/dev/tty && printf '\n' >/dev/tty && ADMIN_ACCESS=lan; }
	fi
	return 0
}

# admin_access_reminder: the one line printed when nobody chose (no terminal, no option).
admin_access_reminder() {
	warn admin_reminder
}
