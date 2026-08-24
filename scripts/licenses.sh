#!/bin/sh
# Print the licence of every Go module linked into the panel, as a markdown
# table for THIRD-PARTY.md. Classification is by the wording of the licence
# file, which is crude but has no dependencies — check anything it calls
# UNKNOWN by hand.
set -eu

go list -deps -f '{{with .Module}}{{.Path}}|{{.Version}}|{{.Dir}}{{end}}' . |
	sort -u | grep -v '^$' | grep -v '^modularnost|' |
	while IFS='|' read -r path version dir; do
		file=""
		d=$dir
		# A submodule keeps its licence in the repository root above it.
		for _ in 1 2 3 4; do
			for name in LICENSE LICENSE.txt LICENSE.md LICENCE COPYING; do
				[ -f "$d/$name" ] && { file="$d/$name"; break; }
			done
			[ -n "$file" ] && break
			d=$(dirname "$d")
		done
		if [ -z "$file" ]; then
			echo "| $path | $version | NOT FOUND |"
			continue
		fi
		head=$(head -c 4000 "$file")
		case "$head" in
		*"Apache License"*) kind="Apache-2.0" ;;
		*"Mozilla Public License"*) kind="MPL-2.0" ;;
		*"MIT License"* | *"Permission is hereby granted, free of charge"*) kind="MIT" ;;
		*"Redistribution and use in source and binary forms"*)
			case "$head" in
			*"Neither the name"* | *"endorse or promote"*) kind="BSD-3-Clause" ;;
			*) kind="BSD-2-Clause" ;;
			esac
			;;
		*"ISC License"*) kind="ISC" ;;
		*) kind="UNKNOWN" ;;
		esac
		notice=""
		[ -f "$(dirname "$file")/NOTICE" ] && notice=" (ships a NOTICE)"
		echo "| $path | $version | $kind$notice |"
	done
