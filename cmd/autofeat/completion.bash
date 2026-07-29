_autofeat_completion() {
	COMPREPLY=()

	local current="${COMP_WORDS[COMP_CWORD]}"
	local command_name="${COMP_WORDS[0]}"
	local command=""
	local first_argument=2

	case "$command_name" in
		afr)
			command="review"
			first_argument=1
			;;
		afl)
			command="list"
			first_argument=1
			;;
		*)
			if ((COMP_CWORD == 1)); then
				local candidate
				for candidate in new open run sync status review teardown list version completion; do
					if [[ "$candidate" == "$current"* ]]; then
						COMPREPLY+=("$candidate")
					fi
				done
				return
			fi
			command="${COMP_WORDS[1]}"
			;;
	esac

	if [[ "$command" == "completion" ]]; then
		if [[ "bash" == "$current"* ]]; then
			COMPREPLY=(bash)
		fi
		return
	fi

	case "$command" in
		open | run | sync | status | review | teardown) ;;
		*) return ;;
	esac

	local previous=""
	if ((COMP_CWORD > 0)); then
		previous="${COMP_WORDS[COMP_CWORD - 1]}"
	fi
	if [[ "$previous" == "--base" || "$previous" == "-task" ]]; then
		return
	fi

	local -a features=()
	local -a selected=()
	local -a candidates=()
	mapfile -t features < <(command autofeat __complete features 2>/dev/null)

	local index
	local skip_value=false
	local force_seen=false
	local base_seen=false
	local task_seen=false
	local word
	for ((index = first_argument; index < COMP_CWORD; index++)); do
		word="${COMP_WORDS[index]}"
		if [[ "$skip_value" == true ]]; then
			skip_value=false
			continue
		fi
		case "$word" in
			--force)
				force_seen=true
				;;
			--base)
				base_seen=true
				skip_value=true
				;;
			-task)
				task_seen=true
				skip_value=true
				;;
			*)
				selected+=("$word")
				;;
		esac
	done

	if [[ "$command" == "run" && "$task_seen" == true ]]; then
		return
	fi

	local feature
	local selected_feature
	local selected_name
	for feature in "${features[@]}"; do
		selected_feature=false
		for selected_name in "${selected[@]}"; do
			if [[ "$feature" == "$selected_name" ]]; then
				selected_feature=true
				break
			fi
		done
		if [[ "$selected_feature" == false ]]; then
			candidates+=("$feature")
		fi
	done

	if [[ "$command" == "teardown" && "$force_seen" == false ]]; then
		candidates+=(--force)
	elif [[ "$command" == "review" && "$base_seen" == false ]]; then
		candidates+=(--base)
	elif [[ "$command" == "run" && "$task_seen" == false && ${#selected[@]} -gt 0 ]]; then
		candidates+=(-task)
	fi

	local candidate
	for candidate in "${candidates[@]}"; do
		if [[ "$candidate" == "$current"* ]]; then
			COMPREPLY+=("$candidate")
		fi
	done
}

complete -F _autofeat_completion autofeat af afr afl