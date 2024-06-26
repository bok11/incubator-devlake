/*
Licensed to the Apache Software Foundation (ASF) under one or more
contributor license agreements.  See the NOTICE file distributed with
this work for additional information regarding copyright ownership.
The ASF licenses this file to You under the Apache License, Version 2.0
(the "License"); you may not use this file except in compliance with
the License.  You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tasks

import (
	"github.com/apache/incubator-devlake/core/models/domainlayer/code"
	"github.com/apache/incubator-devlake/core/models/domainlayer/crossdomain"
	"github.com/apache/incubator-devlake/core/models/domainlayer/ticket"
	"strings"

	"github.com/apache/incubator-devlake/core/dal"
	"github.com/apache/incubator-devlake/core/errors"
	"github.com/apache/incubator-devlake/core/plugin"
	"github.com/apache/incubator-devlake/helpers/pluginhelper/api"
)

var LinkPrToIssueMeta = plugin.SubTaskMeta{
	Name:             "LinkPrToIssue",
	EntryPoint:       LinkPrToIssue,
	EnabledByDefault: true,
	Description:      "Try to link pull requests to issues, according to pull requests' title and description",
	DependencyTables: []string{code.PullRequest{}.TableName(), ticket.Issue{}.TableName()},
	DomainTypes:      []string{plugin.DOMAIN_TYPE_CODE, plugin.DOMAIN_TYPE_TICKET, plugin.DOMAIN_TYPE_CROSS},
	ProductTables:    []string{crossdomain.PullRequestIssue{}.TableName()},
}

func normalizeIssueKey(issueKey string) string {
	issueKey = strings.ReplaceAll(issueKey, "#", "")
	issueKey = strings.TrimSpace(issueKey)
	return issueKey
}

func LinkPrToIssue(taskCtx plugin.SubTaskContext) errors.Error {
	db := taskCtx.GetDal()
	data := taskCtx.GetData().(*LinkerTaskData)
	var clauses = []dal.Clause{
		dal.From(&code.PullRequest{}),
		dal.Join("LEFT JOIN project_mapping pm ON (pm.table = 'cicd_scopes' AND pm.row_id = pull_requests.base_repo_id)"),
		dal.Where("pm.project_name = ?", data.Options.ProjectName),
	}
	cursor, err := db.Cursor(clauses...)
	if err != nil {
		return err
	}

	defer cursor.Close()

	// iterate all rows
	enricher, err := api.NewDataEnricher(api.DataEnricherArgs[code.PullRequest]{
		Ctx:   taskCtx,
		Name:  code.PullRequest{}.TableName(),
		Input: cursor,
		Enrich: func(pullRequest *code.PullRequest) ([]interface{}, errors.Error) {

			issueKey := ""
			for _, text := range []string{pullRequest.Title, pullRequest.Description} {
				issueKey = data.PrToIssueRegexp.FindString(text)
				if issueKey != "" {
					break
				}
			}
			issueKey = normalizeIssueKey(issueKey)
			if issueKey == "" {
				return nil, nil
			}

			issue := &ticket.Issue{}
			if err := db.First(issue, dal.Where("issue_key = ?", issueKey)); err != nil {
				if db.IsErrorNotFound(err) {
					return nil, nil
				}
				return nil, err
			}

			pullRequestIssue := &crossdomain.PullRequestIssue{
				PullRequestId:  pullRequest.Id,
				IssueId:        issue.Id,
				PullRequestKey: pullRequest.PullRequestKey,
				IssueKey:       issueKey,
			}

			return []interface{}{pullRequestIssue}, nil
		},
	})
	if err != nil {
		return err
	}

	return enricher.Execute()
}
