[ -n "$BASH_VERSION" ] && [ -n "$PS1" ] || return 0
[ -n "$__NSL_HOOKED" ] && return 0
__NSL_HOOKED=1
__nsl_last_n=

__nsl_log() {
  local ec=$? n cmd
  read -r n cmd < <(HISTTIMEFORMAT= history 1)
  n="${n:-0}"
  if [ -z "$__nsl_last_n" ]; then __nsl_last_n=$n; return 0; fi
  [ "$n" != "$__nsl_last_n" ] || return 0
  __nsl_last_n=$n
  {
    printf '{"ts":"%s","user":"%s","cwd":"%s","cmd":%s,"exit":%d}\n' \
      "$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)" "$(id -un)" "$PWD" "$(printf '%s' "$cmd" | jq -Rs .)" "$ec" \
      >> /var/log/nsl/commands.jsonl
  } 2>/dev/null
}

shopt -s histappend
PROMPT_COMMAND="__nsl_log${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
