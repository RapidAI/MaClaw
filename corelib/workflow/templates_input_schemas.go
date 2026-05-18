package workflow

// templates_input_schemas.go defines PhaseInputSchema instances for workflow
// templates that use structured AG UI form collection. Each function returns
// a schema that maps directly to AgentViewField[] on the frontend.

func ptrFloat(v float64) *float64 { return &v }
func ptrInt(v int) *int           { return &v }

// codingRequirementsInputSchema returns the structured form for the coding
// workflow's requirements phase.
func codingRequirementsInputSchema() *PhaseInputSchema {
	return &PhaseInputSchema{
		Title:       "Project intake",
		Description: "Capture the product, platform, and implementation constraints before drafting requirements.",
		Fields: []PhaseInputField{
			{Name: "project_name", Label: "Project name", Type: "text", Required: true, Placeholder: "Example: snake game, user management system"},
			{Name: "tech_stack", Label: "Primary tech stack", Type: "select", Required: true, Options: []PhaseInputOption{{Label: "C/C++", Value: "cpp"}, {Label: "Python", Value: "python"}, {Label: "Go", Value: "go"}, {Label: "JavaScript/TypeScript", Value: "js"}, {Label: "Java", Value: "java"}, {Label: "Rust", Value: "rust"}, {Label: "C#/.NET", Value: "csharp"}, {Label: "Other", Value: "other"}}},
			{Name: "platform", Label: "Target platform", Type: "multiselect", Options: []PhaseInputOption{{Label: "Windows", Value: "windows"}, {Label: "macOS", Value: "macos"}, {Label: "Linux", Value: "linux"}, {Label: "Web browser", Value: "web"}, {Label: "Mobile (Android/iOS)", Value: "mobile"}, {Label: "Cross-platform", Value: "cross"}}},
			{Name: "build_tool", Label: "Build tool", Type: "select", Options: []PhaseInputOption{{Label: "Auto-select (recommended)", Value: "auto"}, {Label: "CMake", Value: "cmake"}, {Label: "Makefile", Value: "makefile"}, {Label: "npm/yarn/pnpm", Value: "npm"}, {Label: "Gradle/Maven", Value: "gradle"}, {Label: "Cargo", Value: "cargo"}, {Label: "go mod", Value: "gomod"}, {Label: "Other", Value: "other"}}},
			{Name: "description", Label: "Feature description", Type: "textarea", Required: true, Placeholder: "Describe the expected features, gameplay, UI, interactions, and behavior.", Description: "Be specific about what the software should do and how success should be judged."},
			{Name: "constraints", Label: "Special requirements", Type: "textarea", Placeholder: "Performance, dependency limits, UI style, compatibility, security, or deployment requirements."},
			{Name: "project_path", Label: "Project directory", Type: "text", Placeholder: "Example: D:\\workprj\\my-game", Description: "Leave empty to use the current workspace."},
		},
	}
}

// presentationDesignInputSchema returns the structured form for the
// presentation design workflow's first phase (audience & goal).
func presentationDesignInputSchema() *PhaseInputSchema {
	return &PhaseInputSchema{
		Title:       "Presentation brief",
		Description: "Capture the audience, goal, and content boundaries before planning slides.",
		Fields: []PhaseInputField{
			{Name: "topic", Label: "Presentation topic", Type: "text", Required: true, Placeholder: "Example: Q3 product launch, thesis defense"},
			{Name: "audience", Label: "Audience", Type: "select", Required: true, Options: []PhaseInputOption{{Label: "Internal team", Value: "internal"}, {Label: "Clients or partners", Value: "client"}, {Label: "Investors or executives", Value: "investor"}, {Label: "Academic committee", Value: "academic"}, {Label: "Public or media", Value: "public"}, {Label: "Training or teaching", Value: "training"}, {Label: "Other", Value: "other"}}},
			{Name: "duration", Label: "Talk length", Type: "select", Options: []PhaseInputOption{{Label: "5 minutes", Value: "5min"}, {Label: "10-15 minutes", Value: "15min"}, {Label: "20-30 minutes", Value: "30min"}, {Label: "45-60 minutes", Value: "60min"}, {Label: "Unknown", Value: "unknown"}}},
			{Name: "slide_count", Label: "Expected slide count", Type: "number", Min: ptrFloat(3), Max: ptrFloat(100), Placeholder: "Suggested: 10-30"},
			{Name: "style", Label: "Style preference", Type: "select", Options: []PhaseInputOption{{Label: "Concise business", Value: "business"}, {Label: "Technology-forward", Value: "tech"}, {Label: "Academic and rigorous", Value: "academic"}, {Label: "Creative and energetic", Value: "creative"}, {Label: "No preference / auto", Value: "any"}}},
			{Name: "key_points", Label: "Key points", Type: "textarea", Required: true, Placeholder: "List 3-5 points the deck must cover.", Description: "These points become the backbone of the presentation."},
			{Name: "additional_info", Label: "Additional notes", Type: "textarea", Placeholder: "Brand colors, logo, references, tone, forbidden content, or source material."},
		},
	}
}

// innovationInputSchema returns the structured form for the innovation
// workflow's opportunity identification phase.
func innovationInputSchema() *PhaseInputSchema {
	return &PhaseInputSchema{
		Title:       "Innovation brief",
		Description: "Capture the domain, pain points, and available resources before opportunity analysis.",
		Fields: []PhaseInputField{
			{Name: "domain", Label: "Target domain or industry", Type: "text", Required: true, Placeholder: "Example: smart home, online education, new energy"},
			{Name: "innovation_type", Label: "Innovation type", Type: "select", Required: true, Options: []PhaseInputOption{{Label: "Product innovation", Value: "product"}, {Label: "Technology innovation", Value: "technology"}, {Label: "Business model innovation", Value: "business_model"}, {Label: "Process or efficiency innovation", Value: "process"}, {Label: "Service innovation", Value: "service"}, {Label: "Mixed or unknown", Value: "mixed"}}},
			{Name: "pain_points", Label: "Known pain points or opportunities", Type: "textarea", Required: true, Placeholder: "Describe observed customer needs, market gaps, or technology opportunities."},
			{Name: "resource_level", Label: "Available resources", Type: "select", Options: []PhaseInputOption{{Label: "Individual or small team (1-3 people)", Value: "small"}, {Label: "Medium team (4-10 people)", Value: "medium"}, {Label: "Large team or company", Value: "large"}, {Label: "Unknown", Value: "unknown"}}},
			{Name: "constraints", Label: "Constraints", Type: "textarea", Placeholder: "Budget, timeline, technical, compliance, channel, or operational constraints."},
		},
	}
}

// businessPlanInputSchema returns the structured form for the business plan
// workflow's requirement scoping phase.
func businessPlanInputSchema() *PhaseInputSchema {
	return &PhaseInputSchema{
		Title:       "Business plan brief",
		Description: "Capture the business plan context before market and financial planning.",
		Fields: []PhaseInputField{
			{Name: "project_name", Label: "Project or company name", Type: "text", Required: true, Placeholder: "Example: AI customer support SaaS platform"},
			{Name: "target_audience", Label: "Target reader", Type: "select", Required: true, Options: []PhaseInputOption{{Label: "Investors (angel/VC/PE)", Value: "investor"}, {Label: "Bank or loan reviewer", Value: "bank"}, {Label: "Government grant reviewer", Value: "government"}, {Label: "Internal decision maker", Value: "internal"}, {Label: "Partner or channel", Value: "partner"}}},
			{Name: "stage", Label: "Company stage", Type: "select", Required: true, Options: []PhaseInputOption{{Label: "Concept only", Value: "concept"}, {Label: "Seed / MVP", Value: "seed"}, {Label: "Growth with users or revenue", Value: "growth"}, {Label: "Mature and profitable", Value: "mature"}}},
			{Name: "industry", Label: "Industry", Type: "text", Required: true, Placeholder: "Example: artificial intelligence, healthcare, renewable energy"},
			{Name: "funding_amount", Label: "Funding amount", Type: "text", Placeholder: "Example: 5M RMB, 10M RMB, or leave empty if not applicable"},
			{Name: "doc_length", Label: "Document depth", Type: "select", Options: []PhaseInputOption{{Label: "Brief version (10-15 pages)", Value: "short"}, {Label: "Standard version (20-30 pages)", Value: "standard"}, {Label: "Detailed version (40+ pages)", Value: "detailed"}}},
			{Name: "core_description", Label: "Project summary", Type: "textarea", Required: true, Placeholder: "In 2-3 sentences, explain what it does, what problem it solves, and who it serves."},
		},
	}
}

// testingInputSchema returns the structured form for the testing workflow's
// test strategy phase.
func testingInputSchema() *PhaseInputSchema {
	return &PhaseInputSchema{
		Title:       "Testing brief",
		Description: "Capture the system under test, scope, and risk focus before creating the test strategy.",
		Fields: []PhaseInputField{
			{Name: "project_name", Label: "Project or system name", Type: "text", Required: true, Placeholder: "Example: user management system v2.0"},
			{Name: "test_scope", Label: "Test scope", Type: "multiselect", Required: true, Options: []PhaseInputOption{{Label: "Functional testing", Value: "functional"}, {Label: "Performance testing", Value: "performance"}, {Label: "Security testing", Value: "security"}, {Label: "Compatibility testing", Value: "compatibility"}, {Label: "Regression testing", Value: "regression"}, {Label: "API testing", Value: "api"}, {Label: "UI/UX testing", Value: "ui"}}},
			{Name: "test_method", Label: "Test method", Type: "select", Options: []PhaseInputOption{{Label: "Mostly manual", Value: "manual"}, {Label: "Mostly automated", Value: "automated"}, {Label: "Manual and automated mix", Value: "mixed"}}},
			{Name: "tech_stack", Label: "Tech stack", Type: "text", Placeholder: "Example: React + Node.js + PostgreSQL"},
			{Name: "description", Label: "Testing focus", Type: "textarea", Required: true, Placeholder: "Describe important modules, known issues, risky flows, and special scenarios."},
		},
	}
}

// literatureReviewInputSchema returns the structured form for the literature
// review workflow's topic definition phase.
func literatureReviewInputSchema() *PhaseInputSchema {
	return &PhaseInputSchema{
		Title:       "Literature review brief",
		Description: "Capture the research topic, questions, and search boundaries before review planning.",
		Fields: []PhaseInputField{
			{Name: "research_topic", Label: "Research topic or field", Type: "text", Required: true, Placeholder: "Example: large language models for code generation"},
			{Name: "research_questions", Label: "Research questions", Type: "textarea", Required: true, Placeholder: "List 1-3 questions the review should answer."},
			{Name: "time_range", Label: "Time range", Type: "select", Options: []PhaseInputOption{{Label: "Last 3 years", Value: "3years"}, {Label: "Last 5 years", Value: "5years"}, {Label: "Last 10 years", Value: "10years"}, {Label: "No limit", Value: "unlimited"}}},
			{Name: "language", Label: "Source language", Type: "multiselect", Options: []PhaseInputOption{{Label: "English", Value: "english"}, {Label: "Chinese", Value: "chinese"}, {Label: "Other", Value: "other"}}},
			{Name: "keywords", Label: "Search keywords", Type: "textarea", Placeholder: "List English and Chinese keywords, separated by commas. Leave empty to let the system derive them."},
		},
	}
}

// researchReportInputSchema returns the structured form for the research
// report workflow's requirement scoping phase.
func researchReportInputSchema() *PhaseInputSchema {
	return &PhaseInputSchema{
		Title:       "Research report brief",
		Description: "Capture the industry, dimensions, and desired depth before research planning.",
		Fields: []PhaseInputField{
			{Name: "industry", Label: "Target industry or topic", Type: "text", Required: true, Placeholder: "Example: new energy vehicles, semiconductors, AI foundation models"},
			{Name: "focus_areas", Label: "Focus areas", Type: "multiselect", Required: true, Options: []PhaseInputOption{{Label: "Market size and growth", Value: "market_size"}, {Label: "Competitive landscape", Value: "competition"}, {Label: "Technology trends", Value: "technology"}, {Label: "Policy impact", Value: "policy"}, {Label: "Investment opportunities", Value: "investment"}, {Label: "Supply chain analysis", Value: "supply_chain"}}},
			{Name: "time_range", Label: "Time range", Type: "select", Options: []PhaseInputOption{{Label: "Last 1 month", Value: "1month"}, {Label: "Last 3 months", Value: "3months"}, {Label: "Last 6 months", Value: "6months"}, {Label: "Last 1 year", Value: "1year"}}},
			{Name: "specific_companies", Label: "Companies to watch", Type: "textarea", Placeholder: "List important companies to analyze, if any."},
			{Name: "output_depth", Label: "Output depth", Type: "select", Options: []PhaseInputOption{{Label: "Overview with key conclusions and data", Value: "overview"}, {Label: "Standard with detailed analysis and comparisons", Value: "standard"}, {Label: "Deep with full data and chart plan", Value: "deep"}}},
		},
	}
}

// experimentDesignInputSchema returns the structured form for the experiment
// design workflow's hypothesis formulation phase.
func experimentDesignInputSchema() *PhaseInputSchema {
	return &PhaseInputSchema{
		Title:       "Experiment design brief",
		Description: "Capture the research question, experiment type, and constraints before design.",
		Fields: []PhaseInputField{
			{Name: "research_field", Label: "Research field", Type: "text", Required: true, Placeholder: "Example: materials science, psychology, computer vision"},
			{Name: "research_question", Label: "Research question", Type: "textarea", Required: true, Placeholder: "Describe the question or hypothesis the experiment should test."},
			{Name: "experiment_type", Label: "Experiment type", Type: "select", Options: []PhaseInputOption{{Label: "Randomized controlled trial", Value: "rct"}, {Label: "Quasi-experimental design", Value: "quasi"}, {Label: "Pre/post comparison", Value: "pre_post"}, {Label: "Observational study", Value: "observational"}, {Label: "Simulation experiment", Value: "simulation"}, {Label: "Other or unknown", Value: "other"}}},
			{Name: "available_resources", Label: "Available resources", Type: "textarea", Placeholder: "Describe equipment, samples, datasets, budget, collaborators, or facilities."},
			{Name: "constraints", Label: "Constraints", Type: "textarea", Placeholder: "Timeline, ethics, sample size, data access, compliance, or safety constraints."},
		},
	}
}

// grantProposalInputSchema returns the structured form for the grant proposal
// workflow's topic justification phase.
func grantProposalInputSchema() *PhaseInputSchema {
	return &PhaseInputSchema{
		Title:       "Grant proposal brief",
		Description: "Capture the grant type, field, background, and budget before proposal drafting.",
		Fields: []PhaseInputField{
			{Name: "project_title", Label: "Project title", Type: "text", Required: true, Placeholder: "Example: deep learning methods for protein structure prediction"},
			{Name: "fund_type", Label: "Grant type", Type: "select", Required: true, Options: []PhaseInputOption{{Label: "NSFC Youth Fund", Value: "nsfc_youth"}, {Label: "NSFC General Program", Value: "nsfc_general"}, {Label: "NSFC Key Program", Value: "nsfc_key"}, {Label: "Provincial or city research fund", Value: "provincial"}, {Label: "Enterprise collaboration project", Value: "enterprise"}, {Label: "Other", Value: "other"}}},
			{Name: "research_field", Label: "Research field", Type: "text", Required: true, Placeholder: "Example: computer science / AI, biology / molecular biology"},
			{Name: "research_background", Label: "Research background", Type: "textarea", Required: true, Placeholder: "Briefly describe the current foundation, scientific problem, and motivation."},
			{Name: "duration_years", Label: "Project duration (years)", Type: "number", Min: ptrFloat(1), Max: ptrFloat(5), Placeholder: "Usually 3-5 years"},
			{Name: "budget", Label: "Requested budget", Type: "text", Placeholder: "Example: 300k RMB for youth fund, 800k RMB for general program"},
		},
	}
}

// competitiveAnalysisInputSchema returns the structured form for the competitive
// analysis workflow's first phase.
func competitiveAnalysisInputSchema() *PhaseInputSchema {
	return &PhaseInputSchema{
		Title:       "Competitive analysis brief",
		Description: "Capture your product, competitors, and decision purpose before analysis.",
		Fields: []PhaseInputField{
			{Name: "product_name", Label: "Our product or project", Type: "text", Required: true, Placeholder: "Example: our online collaboration tool"},
			{Name: "competitors", Label: "Main competitors", Type: "textarea", Required: true, Placeholder: "List competitors, one per line.", Description: "Provide at least 2-3 important competitors."},
			{Name: "analysis_dimensions", Label: "Analysis dimensions", Type: "multiselect", Required: true, Options: []PhaseInputOption{{Label: "Feature comparison", Value: "features"}, {Label: "Pricing strategy", Value: "pricing"}, {Label: "User experience", Value: "ux"}, {Label: "Technical architecture", Value: "tech"}, {Label: "Market positioning", Value: "positioning"}, {Label: "Operations strategy", Value: "operations"}, {Label: "Funding and team", Value: "funding"}}},
			{Name: "purpose", Label: "Purpose", Type: "select", Options: []PhaseInputOption{{Label: "Product planning reference", Value: "product_planning"}, {Label: "Investment decision", Value: "investment"}, {Label: "Market entry strategy", Value: "market_entry"}, {Label: "Differentiation positioning", Value: "differentiation"}, {Label: "Other", Value: "other"}}},
		},
	}
}

// eventPlanningInputSchema returns the structured form for the event planning
// workflow's first phase.
func eventPlanningInputSchema() *PhaseInputSchema {
	return &PhaseInputSchema{
		Title:       "Event planning brief",
		Description: "Capture event goals, audience size, time, and budget before planning.",
		Fields: []PhaseInputField{
			{Name: "event_name", Label: "Event name", Type: "text", Required: true, Placeholder: "Example: 2026 annual technology summit"},
			{Name: "event_type", Label: "Event type", Type: "select", Required: true, Options: []PhaseInputOption{{Label: "Conference or summit", Value: "conference"}, {Label: "Product launch", Value: "launch"}, {Label: "Team building or annual meeting", Value: "team_building"}, {Label: "Training or workshop", Value: "workshop"}, {Label: "Exhibition or roadshow", Value: "exhibition"}, {Label: "Online event or livestream", Value: "online"}, {Label: "Other", Value: "other"}}},
			{Name: "expected_attendees", Label: "Expected attendees", Type: "number", Min: ptrFloat(1), Max: ptrFloat(10000), Placeholder: "Example: 200"},
			{Name: "date_range", Label: "Planned time", Type: "text", Placeholder: "Example: mid May 2026, two days"},
			{Name: "budget", Label: "Budget range", Type: "text", Placeholder: "Example: 100k-200k RMB"},
			{Name: "goals", Label: "Event goals", Type: "textarea", Required: true, Placeholder: "Describe goals such as brand exposure, customer conversion, training, or team cohesion."},
		},
	}
}

// projectProposalInputSchema returns the structured form for the project
// proposal workflow's background analysis phase.
func projectProposalInputSchema() *PhaseInputSchema {
	return &PhaseInputSchema{
		Title:       "Project proposal brief",
		Description: "Capture the project problem, scope, stakeholders, budget, and timeline before proposal planning.",
		Fields: []PhaseInputField{
			{Name: "project_name", Label: "Project name", Type: "text", Required: true, Placeholder: "Example: customer management system upgrade"},
			{Name: "project_type", Label: "Project type", Type: "select", Required: true, Options: []PhaseInputOption{{Label: "New product or system development", Value: "new_development"}, {Label: "Existing system upgrade", Value: "upgrade"}, {Label: "Infrastructure construction", Value: "infrastructure"}, {Label: "Process optimization or digital transformation", Value: "optimization"}, {Label: "Research or exploratory project", Value: "research"}, {Label: "Other", Value: "other"}}},
			{Name: "problem_description", Label: "Problem to solve", Type: "textarea", Required: true, Placeholder: "Describe the current issue, pain point, or business need."},
			{Name: "expected_duration", Label: "Expected duration", Type: "text", Placeholder: "Example: 3 months, half a year"},
			{Name: "budget_estimate", Label: "Budget estimate", Type: "text", Placeholder: "Example: 500k-1M RMB"},
			{Name: "stakeholders", Label: "Key stakeholders", Type: "textarea", Placeholder: "List stakeholders and what each cares about."},
		},
	}
}

// paperWritingInputSchema returns the structured form for the paper writing
// workflow's outline design phase.
func paperWritingInputSchema() *PhaseInputSchema {
	return &PhaseInputSchema{
		Title:       "Paper writing brief",
		Description: "Capture the paper type, venue, contribution, and language before outline design.",
		Fields: []PhaseInputField{
			{Name: "paper_title", Label: "Working title", Type: "text", Required: true, Placeholder: "Example: Transformer-based multimodal sentiment analysis"},
			{Name: "paper_type", Label: "Paper type", Type: "select", Required: true, Options: []PhaseInputOption{{Label: "Journal paper", Value: "journal"}, {Label: "Conference paper", Value: "conference"}, {Label: "Thesis", Value: "thesis"}, {Label: "Survey or review", Value: "survey"}, {Label: "Short paper or letter", Value: "short"}}},
			{Name: "target_venue", Label: "Target venue", Type: "text", Placeholder: "Example: IEEE TPAMI, ACL 2026, Nature Communications", Description: "A clear venue helps match format, rigor, and length."},
			{Name: "research_type", Label: "Research type", Type: "select", Required: true, Options: []PhaseInputOption{{Label: "Experimental method and evaluation", Value: "experimental"}, {Label: "Theoretical proof or formalization", Value: "theoretical"}, {Label: "System design and engineering evaluation", Value: "system"}, {Label: "Survey or literature analysis", Value: "survey"}, {Label: "Case study", Value: "case_study"}}},
			{Name: "core_contribution", Label: "Core contribution", Type: "textarea", Required: true, Placeholder: "Summarize the paper's main novelty and contribution in 1-3 sentences."},
			{Name: "writing_language", Label: "Writing language", Type: "select", Required: true, Options: []PhaseInputOption{{Label: "English", Value: "english"}, {Label: "Chinese", Value: "chinese"}}},
			{Name: "existing_materials", Label: "Existing materials", Type: "textarea", Placeholder: "Describe datasets, experiments, code, preliminary results, or notes already available."},
		},
	}
}

// opsMaintenanceInputSchema returns the structured form for the ops maintenance
// workflow's intake phase.
func opsMaintenanceInputSchema() *PhaseInputSchema {
	return &PhaseInputSchema{
		Title:       "Operations request brief",
		Description: "Capture the target, environment, risk, and desired execution mode before planning operations work.",
		Fields: []PhaseInputField{
			{Name: "operation_description", Label: "Operation description", Type: "textarea", Required: true, Placeholder: "Describe the action, such as cleaning /tmp, restarting nginx, or updating a Docker image."},
			{Name: "target_host", Label: "Target host", Type: "text", Required: true, Placeholder: "Example: api.example.com, 192.168.1.100", Description: "Use comma-separated values for multiple hosts."},
			{Name: "environment", Label: "Environment", Type: "select", Required: true, Options: []PhaseInputOption{{Label: "Development", Value: "dev"}, {Label: "Testing", Value: "test"}, {Label: "Staging", Value: "staging"}, {Label: "Production", Value: "prod"}, {Label: "Critical infrastructure", Value: "critical"}, {Label: "Unknown", Value: "unknown"}}},
			{Name: "execution_mode", Label: "Execution mode", Type: "select", Options: []PhaseInputOption{{Label: "Generate documentation or scripts only", Value: "document_only"}, {Label: "Generate a plan and execute only after approval", Value: "execute_after_approval"}, {Label: "Auto-execute low-risk operations", Value: "auto_execute"}}},
			{Name: "urgency", Label: "Urgency", Type: "select", Options: []PhaseInputOption{{Label: "Routine maintenance", Value: "routine"}, {Label: "Planned change", Value: "planned"}, {Label: "Urgent fix", Value: "urgent"}, {Label: "Incident response", Value: "emergency"}}},
			{Name: "additional_context", Label: "Additional context", Type: "textarea", Placeholder: "Relevant logs, alerts, previous actions, rollback expectations, or approvals."},
		},
	}
}

// changjiangScholarInputSchema returns the structured form for the Changjiang
// Scholar application workflow's personal profile phase.
func changjiangScholarInputSchema() *PhaseInputSchema {
	return &PhaseInputSchema{
		Title:       "Changjiang Scholar applicant profile",
		Description: "Capture the applicant profile and academic highlights before drafting application materials.",
		Fields: []PhaseInputField{
			{Name: "name", Label: "Applicant name", Type: "text", Required: true, Placeholder: "Applicant full name"},
			{Name: "gender", Label: "Gender", Type: "select", Required: true, Options: []PhaseInputOption{{Label: "Male", Value: "male"}, {Label: "Female", Value: "female"}}},
			{Name: "birth_date", Label: "Birth date", Type: "text", Required: true, Placeholder: "Example: May 1985"},
			{Name: "position_type", Label: "Application category", Type: "select", Required: true, Options: []PhaseInputOption{{Label: "Distinguished professor", Value: "distinguished"}, {Label: "Young scholar", Value: "young_scholar"}, {Label: "Chair professor", Value: "chair_professor"}}},
			{Name: "discipline_category", Label: "Discipline category", Type: "select", Required: true, Options: []PhaseInputOption{{Label: "Natural sciences", Value: "natural_science"}, {Label: "Engineering", Value: "engineering"}, {Label: "Humanities and social sciences", Value: "humanities"}, {Label: "Medicine", Value: "medical"}}},
			{Name: "research_field", Label: "Research direction", Type: "text", Required: true, Placeholder: "Example: artificial intelligence and machine learning, quantum computing, molecular biology"},
			{Name: "current_institution", Label: "Current institution", Type: "text", Required: true, Placeholder: "Example: XX University, XX School"},
			{Name: "current_title", Label: "Current title", Type: "select", Required: true, Options: []PhaseInputOption{{Label: "Professor / researcher", Value: "professor"}, {Label: "Associate professor / associate researcher", Value: "associate_professor"}, {Label: "Overseas associate professor or above", Value: "overseas_associate"}}},
			{Name: "education_background", Label: "Education background", Type: "textarea", Required: true, Placeholder: "List in chronological order:\nBachelor: XX University, major, years\nMaster: XX University, major, years\nPhD: XX University, major, years", Description: "Include undergraduate, master's, and doctoral education when available."},
			{Name: "key_achievements", Label: "Key academic achievements", Type: "textarea", Required: true, Placeholder: "List 3-5 major achievements, such as high-impact papers, national projects, awards, or field contributions.", Description: "Later phases can expand these; this field should capture the strongest highlights."},
			{Name: "h_index", Label: "H-index", Type: "number", Min: ptrFloat(0), Max: ptrFloat(200), Placeholder: "Example: 35"},
			{Name: "total_papers", Label: "Total SCI/SSCI papers", Type: "number", Min: ptrFloat(0), Placeholder: "Example: 120"},
		},
	}
}

// productDesignInputSchema returns the structured form for the product design
// workflow's problem discovery phase.
func productDesignInputSchema() *PhaseInputSchema {
	return &PhaseInputSchema{
		Title:       "Product design brief",
		Description: "Capture the product direction, users, problem, and current stage before product design.",
		Fields: []PhaseInputField{
			{Name: "product_name", Label: "Product name or direction", Type: "text", Required: true, Placeholder: "Example: online whiteboard, AI customer support system"},
			{Name: "product_type", Label: "Product type", Type: "select", Required: true, Options: []PhaseInputOption{{Label: "Web app", Value: "web_app"}, {Label: "Mobile app", Value: "mobile_app"}, {Label: "Desktop software", Value: "desktop"}, {Label: "Mini program", Value: "mini_program"}, {Label: "SaaS platform", Value: "saas"}, {Label: "Hardware product", Value: "hardware"}, {Label: "Other", Value: "other"}}},
			{Name: "target_users", Label: "Target users", Type: "textarea", Required: true, Placeholder: "Describe who the users are, their characteristics, and their usage scenarios."},
			{Name: "core_problem", Label: "Core problem", Type: "textarea", Required: true, Placeholder: "What pain point do users face, and why are current solutions insufficient?"},
			{Name: "competitors", Label: "Known competitors", Type: "textarea", Placeholder: "List known competitors or substitute solutions."},
			{Name: "stage", Label: "Current stage", Type: "select", Options: []PhaseInputOption{{Label: "Starting from concept", Value: "concept"}, {Label: "Initial idea exists", Value: "idea"}, {Label: "MVP or prototype exists", Value: "mvp"}, {Label: "Launched and iterating", Value: "iteration"}}},
		},
	}
}
