package main

import (
	"reflect"
	"testing"
)

func Test_sortPackages(t *testing.T) {
	tests := []struct {
		name string
		packages []*apiPackage
		expected []*apiPackage
	}{
		{
			name: "sort by group then version",
			packages: []*apiPackage{
				{
					apiGroup:   "a",
					apiVersion: "v1",
				},
				{
					apiGroup:   "c",
					apiVersion: "v1beta1",
				},
				{
					apiGroup:   "b",
					apiVersion: "v1beta1",
				},
				{
					apiGroup:   "c",
					apiVersion: "v1",
				},
				{
					apiGroup:   "b",
					apiVersion: "v1",
				},
				{
					apiGroup:   "a",
					apiVersion: "v1beta1",
				},
			},
			expected: []*apiPackage{
				{
					apiGroup:   "a",
					apiVersion: "v1",
				},
				{
					apiGroup:   "a",
					apiVersion: "v1beta1",
				},
				{
					apiGroup:   "b",
					apiVersion: "v1",
				},
				{
					apiGroup:   "b",
					apiVersion: "v1beta1",
				},
				{
					apiGroup:   "c",
					apiVersion: "v1",
				},
				{
					apiGroup:   "c",
					apiVersion: "v1beta1",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortPackages(tt.packages)
			if !reflect.DeepEqual(tt.expected, tt.packages) {
				t.Error("Unexpected packages ordering")
			}
		})
	}
}