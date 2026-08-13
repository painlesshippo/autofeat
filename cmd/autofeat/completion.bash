_autofeat_completion() {
	COMPREPLY=()

	local current="${COMP_WORDS[COMP_CWORD]}"
	local command_name="${COMP_WORDS[0]}"
	local command=""
	local first_argument=2

	case "$command_name" in
		afl)
			command="list"
			first_argument=1
			;;
		*)
			if ((COMP_CWORD == 1)); then
				local candidate
				for candidate in new open run sync status teardown list template config version completion; do
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
	if [[ "$command" == "new" ]]; then
		if [[ "${COMP_WORDS[COMP_CWORD - 1]}" == "--template" ]]; then
			local -a templates=()
			mapfile -t templates < <(command autofeat __complete templates 2>/dev/null)
			local template_name
			for template_name in "${templates[@]}"; do
				if [[ "$template_name" == "$current"* ]]; then
					COMPREPLY+=("$template_name")
				fi
			done
		elif ((COMP_CWORD == 3)) && [[ "--template" == "$current"* ]]; then
			COMPREPLY=(--template)
		fi
		return
	fi
	if [[ "$command" == "template" ]]; then
		if ((COMP_CWORD == 2)); then
			local candidate
			for candidate in list show save; do
				if [[ "$candidate" == "$current"* ]]; then
					COMPREPLY+=("$candidate")
				fi
			done
		elif [[ "${COMP_WORDS[2]}" == "show" ]] && ((COMP_CWORD == 3)); then
			local -a templates=()
			mapfile -t templates < <(command autofeat __complete templates 2>/dev/null)
			local template_name
			for template_name in "${templates[@]}"; do
				if [[ "$template_name" == "$current"* ]]; then
					COMPREPLY+=("$template_name")
				fi
			done
		elif [[ "${COMP_WORDS[2]}" == "save" ]] && ((COMP_CWORD == 4)); then
			if [[ "--from" == "$current"* ]]; then
				COMPREPLY=(--from)
			fi
		elif [[ "${COMP_WORDS[2]}" == "save" && "${COMP_WORDS[COMP_CWORD - 1]}" == "--from" ]]; then
			local -a features=()
			mapfile -t features < <(command autofeat __complete features 2>/dev/null)
			local feature
			for feature in "${features[@]}"; do
				if [[ "$feature" == "$current"* ]]; then
					COMPREPLY+=("$feature")
				fi
			done
		fi
		return
	fi

	case "$command" in
		open | run | sync | status | teardown) ;;
		*) return ;;
	esac

	local previous=""
	if ((COMP_CWORD > 0)); then
		previous="${COMP_WORDS[COMP_CWORD - 1]}"
	fi
	if [[ "$previous" == "-task" ]]; then
		return
	fi

	local -a features=()
	local -a selected=()
	local -a candidates=()
	mapfile -t features < <(command autofeat __complete features 2>/dev/null)

	local index
	local skip_value=false
	local copilot_seen=false
	local force_seen=false
	local task_seen=false
	local word
	for ((index = first_argument; index < COMP_CWORD; index++)); do
		word="${COMP_WORDS[index]}"
		if [[ "$skip_value" == true ]]; then
			skip_value=false
			continue
		fi
		case "$word" in
			--copilot)
				copilot_seen=true
				;;
			--force)
				force_seen=true
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

	if [[ "$command" == "open" && "$copilot_seen" == false ]]; then
		candidates+=(--copilot)
	elif [[ "$command" == "teardown" && "$force_seen" == false ]]; then
		candidates+=(--force)
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

complete -F _autofeat_completion autofeat af afl
