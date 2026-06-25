package v1

import (
	"log"
	"net/http"
	"strings"

	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/gin-gonic/gin"
)

// PromptHandler manages prompt template CRUD.
type PromptHandler struct {
	promptRepo *repository.PromptRepo
}

// NewPromptHandler creates a new PromptHandler.
func NewPromptHandler(promptRepo *repository.PromptRepo) *PromptHandler {
	return &PromptHandler{promptRepo: promptRepo}
}

// List returns all prompt templates (GET /prompts).
func (h *PromptHandler) List(c *gin.Context) {
	templates, err := h.promptRepo.GetAll()
	if err != nil {
		log.Printf("service=prompt-handler action=list error=%v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"templates": templates})
}

// Get returns a single prompt template by name (GET /prompts/:name).
func (h *PromptHandler) Get(c *gin.Context) {
	name := c.Param("name")
	tpl, err := h.promptRepo.Get(name)
	if err != nil {
		log.Printf("service=prompt-handler action=get name=%s error=%v", name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	if tpl == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "模板不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"template": tpl})
}

// Save creates or updates a prompt template (POST /prompts).
func (h *PromptHandler) Save(c *gin.Context) {
	var body struct {
		Name        string `json:"name" binding:"required"`
		Title       string `json:"title" binding:"required"`
		Content     string `json:"content" binding:"required"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name, title, content are required"})
		return
	}

	name := strings.TrimSpace(body.Name)
	content := strings.TrimSpace(body.Content)
	if name == "" || content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "名称和内容不能为空"})
		return
	}

	var desc *string
	if body.Description != "" {
		desc = &body.Description
	}

	result, err := h.promptRepo.Upsert(name, strings.TrimSpace(body.Title), content, desc)
	if err != nil {
		log.Printf("service=prompt-handler action=save name=%s error=%v", name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}

	log.Printf("service=prompt-handler action=save name=%s result=success", name)
	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"template": result,
	})
}

// Delete removes a prompt template by name (DELETE /prompts/:name).
func (h *PromptHandler) Delete(c *gin.Context) {
	name := c.Param("name")
	deleted, err := h.promptRepo.Delete(name)
	if err != nil {
		log.Printf("service=prompt-handler action=delete name=%s error=%v", name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	if !deleted {
		c.JSON(http.StatusNotFound, gin.H{"error": "模板不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
