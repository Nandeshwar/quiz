package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	mathrand "math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

//go:embed data/*.json static/index.html static/contact.html
var appFS embed.FS

const defaultZip = "80134"

type dataset struct {
	Version   string     `json:"version"`
	Locality  string     `json:"locality"`
	AsOf      string     `json:"asOf"`
	Questions []question `json:"questions"`
}

type question struct {
	ID              int      `json:"id"`
	Question        string   `json:"question"`
	Answers         []string `json:"answers"`
	RequiredAnswers int      `json:"requiredAnswers"`
}

type stateInfo struct {
	State    string   `json:"state"`
	Capital  string   `json:"capital"`
	Governor string   `json:"governor"`
	Senators []string `json:"senators"`
}

type locationContext struct {
	Zip                    string   `json:"zip"`
	City                   string   `json:"city"`
	State                  string   `json:"state"`
	StateCode              string   `json:"stateCode"`
	Locality               string   `json:"locality"`
	RepresentativeDistrict string   `json:"representativeDistrict"`
	Representatives        []string `json:"representatives"`
	Senators               []string `json:"senators"`
	Governor               string   `json:"governor"`
	Capital                string   `json:"capital"`
}

type locationResolver struct {
	client *http.Client
	states map[string]stateInfo

	mu    sync.RWMutex
	cache map[string]locationContext
}

type zipAPIResponse struct {
	Country  string `json:"country"`
	PostCode string `json:"post code"`
	Places   []struct {
		PlaceName         string `json:"place name"`
		State             string `json:"state"`
		StateAbbreviation string `json:"state abbreviation"`
	} `json:"places"`
}

type questionResponse struct {
	Version         string   `json:"version"`
	Locality        string   `json:"locality"`
	AsOf            string   `json:"asOf"`
	ID              int      `json:"id"`
	Question        string   `json:"question"`
	Answers         []string `json:"answers"`
	RequiredAnswers int      `json:"requiredAnswers"`
}

type quizQuestion struct {
	ID              int    `json:"id"`
	Question        string `json:"question"`
	RequiredAnswers int    `json:"requiredAnswers"`
}

type startQuizRequest struct {
	Count int    `json:"count"`
	Zip   string `json:"zip"`
}

type answerRequest struct {
	QuestionID int    `json:"questionId"`
	Answer     string `json:"answer"`
}

type answerResult struct {
	QuestionID      int      `json:"questionId"`
	Question        string   `json:"question"`
	UserAnswer      string   `json:"userAnswer"`
	AcceptedAnswers []string `json:"acceptedAnswers"`
	Correct         bool     `json:"correct"`
}

type quizSession struct {
	ID          string
	Version     string
	Zip         string
	Locality    string
	QuestionIDs []int
	Questions   []question
	Answers     map[int]answerResult
	CreatedAt   time.Time
}

type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*quizSession
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: make(map[string]*quizSession)}
}

func (s *sessionStore) create(version, zip, locality string, questions []question, questionIDs []int) *quizSession {
	session := &quizSession{
		ID:          newSessionID(),
		Version:     version,
		Zip:         zip,
		Locality:    locality,
		QuestionIDs: questionIDs,
		Questions:   questions,
		Answers:     make(map[int]answerResult),
		CreatedAt:   time.Now().UTC(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
	return session
}

func (s *sessionStore) get(id string) (*quizSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	return session, ok
}

func loadDataset(name string) (dataset, error) {
	var ds dataset
	raw, err := appFS.ReadFile(name)
	if err != nil {
		return ds, err
	}
	err = json.Unmarshal(raw, &ds)
	return ds, err
}

func loadPage(name string) (string, error) {
	raw, err := appFS.ReadFile(name)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func loadStateMeta() (map[string]stateInfo, error) {
	raw, err := appFS.ReadFile("data/state_meta.json")
	if err != nil {
		return nil, err
	}

	var meta map[string]stateInfo
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, err
	}
	return meta, nil
}

func newLocationResolver(states map[string]stateInfo) *locationResolver {
	return &locationResolver{
		client: &http.Client{Timeout: 10 * time.Second},
		states: states,
		cache:  make(map[string]locationContext),
	}
}

func (r *locationResolver) Resolve(zip string) (locationContext, error) {
	zip = normalizeZip(zip)
	if zip == "" {
		zip = defaultZip
	}

	r.mu.RLock()
	if cached, ok := r.cache[zip]; ok {
		r.mu.RUnlock()
		return cached, nil
	}
	r.mu.RUnlock()

	loc, err := r.fetchLocation(zip)
	if err != nil {
		return locationContext{}, err
	}

	r.mu.Lock()
	r.cache[zip] = loc
	r.mu.Unlock()
	return loc, nil
}

func normalizeZip(zip string) string {
	zip = strings.TrimSpace(zip)
	return zipOnlyRE.FindString(zip)
}

func (r *locationResolver) fetchLocation(zip string) (locationContext, error) {
	req, err := http.NewRequest(http.MethodGet, "https://api.zippopotam.us/us/"+url.PathEscape(zip), nil)
	if err != nil {
		return locationContext{}, err
	}
	res, err := r.client.Do(req)
	if err != nil {
		return locationContext{}, fmt.Errorf("lookup ZIP %s: %w", zip, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return locationContext{}, fmt.Errorf("lookup ZIP %s: status %d", zip, res.StatusCode)
	}

	var payload zipAPIResponse
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return locationContext{}, err
	}
	if len(payload.Places) == 0 {
		return locationContext{}, fmt.Errorf("lookup ZIP %s: no place data", zip)
	}

	city := payload.Places[0].PlaceName
	state := payload.Places[0].State
	stateCode := payload.Places[0].StateAbbreviation
	stateMeta, ok := r.states[stateCode]
	if !ok {
		return locationContext{}, fmt.Errorf("unsupported state for ZIP %s", zip)
	}

	reps, district, err := r.fetchRepresentatives(zip)
	if err != nil {
		return locationContext{}, err
	}

	locality := strings.TrimSpace(fmt.Sprintf("%s, %s %s", city, state, zip))
	return locationContext{
		Zip:                    zip,
		City:                   city,
		State:                  state,
		StateCode:              stateCode,
		Locality:               locality,
		RepresentativeDistrict: district,
		Representatives:        reps,
		Senators:               stateMeta.Senators,
		Governor:               stateMeta.Governor,
		Capital:                stateMeta.Capital,
	}, nil
}

func main() {
	v1, err := loadDataset("data/v1.json")
	if err != nil {
		log.Fatal(err)
	}
	v2, err := loadDataset("data/v2.json")
	if err != nil {
		log.Fatal(err)
	}
	stateMeta, err := loadStateMeta()
	if err != nil {
		log.Fatal(err)
	}
	indexPage, err := loadPage("static/index.html")
	if err != nil {
		log.Fatal(err)
	}
	contactPage, err := loadPage("static/contact.html")
	if err != nil {
		log.Fatal(err)
	}

	datasets := map[string]dataset{
		"v1": v1,
		"v2": v2,
	}

	store := newSessionStore()
	resolver := newLocationResolver(stateMeta)

	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	api := e.Group("/api")
	api.GET("/location", func(c echo.Context) error {
		loc, err := resolver.Resolve(c.QueryParam("zip"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return c.JSON(http.StatusOK, loc)
	})
	registerVersionRoutes(api.Group("/v1"), v1, store, resolver)
	registerVersionRoutes(api.Group("/v2"), v2, store, resolver)

	e.GET("/", func(c echo.Context) error {
		return c.HTML(http.StatusOK, indexPage)
	})
	e.GET("/contact", func(c echo.Context) error {
		return c.HTML(http.StatusOK, contactPage)
	})

	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{
			"status":   "ok",
			"datasets": []string{datasets["v1"].Version, datasets["v2"].Version},
		})
	})

	logRoutes()
	log.Fatal(e.Start(":9999"))
}

func logRoutes() {
	log.Println("Available APIs:")
	for _, route := range []string{
		"GET    /",
		"GET    /contact",
		"GET    /health",
		"GET    /api/location?zip=80134",
		"GET    /api/v1/questions",
		"GET    /api/v1/questions/61?zip=80134",
		`POST   /api/v1/startQuiz {"count":10,"zip":"80134"}`,
		"POST   /api/v1/startQuiz/{sessionID}/answer",
		"GET    /api/v1/startQuiz/{sessionID}/result",
		"GET    /api/v2/questions",
		"GET    /api/v2/questions/43?zip=80134",
		`POST   /api/v2/startQuiz {"count":10,"zip":"80134"}`,
		"POST   /api/v2/startQuiz/{sessionID}/answer",
		"GET    /api/v2/startQuiz/{sessionID}/result",
	} {
		log.Printf("  %s", route)
	}
}

func registerVersionRoutes(group *echo.Group, ds dataset, store *sessionStore, resolver *locationResolver) {
	group.GET("/questions", func(c echo.Context) error {
		localized, loc, err := resolveDatasetForRequest(ds, resolver, c.QueryParam("zip"))
		if err != nil {
			return err
		}
		items := make([]questionResponse, 0, len(localized.Questions))
		for _, q := range localized.Questions {
			items = append(items, toQuestionResponse(localized, q))
		}
		return c.JSON(http.StatusOK, map[string]any{
			"version":   localized.Version,
			"locality":  localized.Locality,
			"asOf":      localized.AsOf,
			"location":  loc,
			"count":     len(items),
			"questions": items,
		})
	})

	group.GET("/questions/:id", func(c echo.Context) error {
		localized, _, err := resolveDatasetForRequest(ds, resolver, c.QueryParam("zip"))
		if err != nil {
			return err
		}
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid question id")
		}
		q, ok := findQuestion(localized, id)
		if !ok {
			return echo.NewHTTPError(http.StatusNotFound, "question not found")
		}
		return c.JSON(http.StatusOK, toQuestionResponse(localized, q))
	})

	group.POST("/startQuiz", func(c echo.Context) error {
		var req startQuizRequest
		if err := bindJSON(c, &req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		localized, loc, err := resolveDatasetForRequest(ds, resolver, req.Zip)
		if err != nil {
			return err
		}
		count := req.Count
		if count <= 0 {
			count = 10
		}
		if count > len(localized.Questions) {
			count = len(localized.Questions)
		}

		ids := randomQuestionIDs(localized.Questions, count)
		session := store.create(ds.Version, loc.Zip, loc.Locality, localized.Questions, ids)

		questions := make([]quizQuestion, 0, len(ids))
		for _, id := range ids {
			q, _ := findQuestion(localized, id)
			questions = append(questions, quizQuestion{
				ID:              q.ID,
				Question:        q.Question,
				RequiredAnswers: q.RequiredAnswers,
			})
		}

		return c.JSON(http.StatusOK, map[string]any{
			"sessionId":      session.ID,
			"version":        localized.Version,
			"locality":       localized.Locality,
			"location":       loc,
			"zip":            loc.Zip,
			"asOf":           localized.AsOf,
			"totalQuestions": len(questions),
			"questions":      questions,
		})
	})

	group.POST("/startQuiz/:sessionID/answer", func(c echo.Context) error {
		session, err := getSession(store, ds.Version, c.Param("sessionID"))
		if err != nil {
			return err
		}

		var req answerRequest
		if err := bindJSON(c, &req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		if !slices.Contains(session.QuestionIDs, req.QuestionID) {
			return echo.NewHTTPError(http.StatusBadRequest, "question is not part of this quiz")
		}

		q, ok := findQuestionInList(session.Questions, req.QuestionID)
		if !ok {
			return echo.NewHTTPError(http.StatusNotFound, "question not found")
		}

		result := answerResult{
			QuestionID:      q.ID,
			Question:        q.Question,
			UserAnswer:      req.Answer,
			AcceptedAnswers: q.Answers,
			Correct:         matchesAnswer(q, req.Answer),
		}

		store.mu.Lock()
		session.Answers[q.ID] = result
		answered := len(session.Answers)
		total := len(session.QuestionIDs)
		store.mu.Unlock()

		return c.JSON(http.StatusOK, map[string]any{
			"sessionId":  session.ID,
			"questionId": q.ID,
			"correct":    result.Correct,
			"answered":   answered,
			"remaining":  total - answered,
		})
	})

	group.GET("/startQuiz/:sessionID/result", func(c echo.Context) error {
		session, err := getSession(store, ds.Version, c.Param("sessionID"))
		if err != nil {
			return err
		}

		store.mu.RLock()
		results := make([]answerResult, 0, len(session.Answers))
		score := 0
		for _, id := range session.QuestionIDs {
			if result, ok := session.Answers[id]; ok {
				results = append(results, result)
				if result.Correct {
					score++
				}
			}
		}
		total := len(session.QuestionIDs)
		answered := len(session.Answers)
		store.mu.RUnlock()

		percentage := 0.0
		if total > 0 {
			percentage = math.Round((float64(score)/float64(total))*10000) / 100
		}

		return c.JSON(http.StatusOK, map[string]any{
			"sessionId":  session.ID,
			"version":    ds.Version,
			"zip":        session.Zip,
			"locality":   session.Locality,
			"completed":  answered == total,
			"answered":   answered,
			"total":      total,
			"score":      score,
			"percentage": percentage,
			"results":    results,
		})
	})
}

func resolveDatasetForRequest(ds dataset, resolver *locationResolver, zip string) (dataset, locationContext, error) {
	loc, err := resolver.Resolve(zip)
	if err != nil {
		return dataset{}, locationContext{}, echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return localizeDataset(ds, loc), loc, nil
}

func getSession(store *sessionStore, version, id string) (*quizSession, error) {
	session, ok := store.get(id)
	if !ok {
		return nil, echo.NewHTTPError(http.StatusNotFound, "quiz session not found")
	}
	if session.Version != version {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "quiz session version mismatch")
	}
	return session, nil
}

func bindJSON(c echo.Context, dst any) error {
	if c.Request().Body == nil || c.Request().Body == http.NoBody {
		return nil
	}
	defer c.Request().Body.Close()
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return errors.New("invalid JSON body")
	}
	return nil
}

func findQuestion(ds dataset, id int) (question, bool) {
	return findQuestionInList(ds.Questions, id)
}

func findQuestionInList(items []question, id int) (question, bool) {
	for _, q := range items {
		if q.ID == id {
			return q, true
		}
	}
	return question{}, false
}

var (
	zipOnlyRE  = regexp.MustCompile(`\d{5}`)
	repNameRE  = regexp.MustCompile(`(?s)<p class="rep[^"]*">.*?<a href="https?://[^"]+">([^<]+)</a><br>`)
	districtRE = regexp.MustCompile(`(?s)is located in the\s+(.+?)\.`)
	spaceRE    = regexp.MustCompile(`\s+`)
)

func (r *locationResolver) fetchRepresentatives(zip string) ([]string, string, error) {
	req, err := http.NewRequest(http.MethodGet, "https://ziplook.house.gov/htbin/findrep_house?ZIP="+url.QueryEscape(zip), nil)
	if err != nil {
		return nil, "", err
	}
	res, err := r.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("lookup representative for ZIP %s: %w", zip, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("lookup representative for ZIP %s: status %d", zip, res.StatusCode)
	}

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, "", err
	}
	html := string(bodyBytes)

	district := ""
	if matches := districtRE.FindStringSubmatch(html); len(matches) > 1 {
		district = spaceRE.ReplaceAllString(strings.TrimSpace(matches[1]), " ")
	}

	rawNames := repNameRE.FindAllStringSubmatch(html, -1)
	names := make([]string, 0, len(rawNames))
	seen := make(map[string]struct{})
	for _, match := range rawNames {
		if len(match) < 2 {
			continue
		}
		name := strings.TrimSpace(spaceRE.ReplaceAllString(match[1], " "))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, district, fmt.Errorf("lookup representative for ZIP %s: no representatives found", zip)
	}
	return names, district, nil
}

func localizeDataset(ds dataset, loc locationContext) dataset {
	out := ds
	out.Locality = loc.Locality
	out.Questions = make([]question, len(ds.Questions))
	copy(out.Questions, ds.Questions)

	for i := range out.Questions {
		switch ds.Version {
		case "v1":
			switch out.Questions[i].ID {
			case 23:
				out.Questions[i].Answers = cloneStrings(loc.Senators)
			case 29:
				out.Questions[i].Answers = cloneStrings(loc.Representatives)
			case 61:
				out.Questions[i].Answers = buildNameAnswers(loc.Governor, "Governor")
			case 62:
				out.Questions[i].Answers = cloneStrings([]string{loc.Capital})
			}
		case "v2":
			switch out.Questions[i].ID {
			case 20:
				out.Questions[i].Answers = cloneStrings(loc.Senators)
			case 23:
				out.Questions[i].Answers = cloneStrings(loc.Representatives)
			case 43:
				out.Questions[i].Answers = buildNameAnswers(loc.Governor, "Governor")
			case 44:
				out.Questions[i].Answers = cloneStrings([]string{loc.Capital})
			}
		}
	}

	return out
}

func cloneStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, len(items))
	copy(out, items)
	return out
}

func buildNameAnswers(name, title string) []string {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	return []string{name, fmt.Sprintf("%s %s", title, name)}
}

func toQuestionResponse(ds dataset, q question) questionResponse {
	return questionResponse{
		Version:         ds.Version,
		Locality:        ds.Locality,
		AsOf:            ds.AsOf,
		ID:              q.ID,
		Question:        q.Question,
		Answers:         q.Answers,
		RequiredAnswers: q.RequiredAnswers,
	}
}

func randomQuestionIDs(questions []question, count int) []int {
	indexes := make([]int, len(questions))
	for i := range questions {
		indexes[i] = i
	}
	mathrand.Shuffle(len(indexes), func(i, j int) {
		indexes[i], indexes[j] = indexes[j], indexes[i]
	})

	ids := make([]int, 0, count)
	for _, idx := range indexes[:count] {
		ids = append(ids, questions[idx].ID)
	}
	return ids
}

func matchesAnswer(q question, userAnswer string) bool {
	parts := splitAnswers(userAnswer)
	if len(parts) == 0 {
		return false
	}

	matched := make(map[string]struct{})
	for _, part := range parts {
		for _, accepted := range q.Answers {
			if equivalent(part, accepted) {
				matched[normalize(accepted)] = struct{}{}
			}
		}
	}

	return len(matched) >= q.RequiredAnswers
}

func splitAnswers(input string) []string {
	normalized := strings.ReplaceAll(input, ";", ",")
	normalized = strings.ReplaceAll(normalized, "\n", ",")
	normalized = regexp.MustCompile(`\s+(and|&)\s+`).ReplaceAllString(normalized, ",")

	rawParts := strings.Split(normalized, ",")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 && strings.TrimSpace(input) != "" {
		return []string{strings.TrimSpace(input)}
	}
	return parts
}

var scrubber = regexp.MustCompile(`[^a-z0-9 ]+`)

func normalize(input string) string {
	input = strings.ToLower(input)
	input = strings.ReplaceAll(input, "u.s.", "us")
	input = strings.ReplaceAll(input, "u.s", "us")
	input = strings.ReplaceAll(input, "’", "'")
	input = strings.ReplaceAll(input, "“", "")
	input = strings.ReplaceAll(input, "”", "")
	input = strings.ReplaceAll(input, "(", " ")
	input = strings.ReplaceAll(input, ")", " ")
	input = strings.ReplaceAll(input, "/", " ")
	input = scrubber.ReplaceAllString(input, " ")
	input = strings.Join(strings.Fields(input), " ")
	return input
}

func equivalent(user, accepted string) bool {
	u := normalize(user)
	a := normalize(accepted)
	if u == "" || a == "" {
		return false
	}
	if u == a || strings.Contains(a, u) || strings.Contains(u, a) {
		return true
	}

	acceptedWords := strings.Fields(a)
	if len(acceptedWords) == 0 {
		return false
	}

	userWordSet := make(map[string]struct{}, len(strings.Fields(u)))
	for _, word := range strings.Fields(u) {
		userWordSet[word] = struct{}{}
	}

	matchedWords := 0
	for _, word := range acceptedWords {
		if _, ok := userWordSet[word]; ok {
			matchedWords++
		}
	}

	return float64(matchedWords)/float64(len(acceptedWords)) >= 0.75
}

func newSessionID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(buf[:])
}
