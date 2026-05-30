<template>
  <section class="grid cols-2">
    <div class="panel">
      <h2>项目目标</h2>
      <p>AntiFraud-KnowledgeHub 面向公共安全教育和开发者集成，提供中文诈骗场景知识库与可解释文本风险分析。</p>
      <el-alert title="MIT License" type="success" :closable="false" />
      <h3>贡献方式</h3>
      <ul>
        <li>新增诈骗分类、风险规则和匿名案例。</li>
        <li>修正规则误报并补充测试样本。</li>
        <li>补充多语言数据和集成示例。</li>
      </ul>
    </div>
    <div class="panel">
      <h2>API 集成示例</h2>
      <pre><code>curl -X POST http://localhost:8080/api/v1/analysis/text \
  -H "Content-Type: application/json" \
  -d '{"text":"客服说账户异常，需要转账到安全账户"}'</code></pre>
      <el-link href="https://github.com/your-org/antifraud-knowledgehub" target="_blank">GitHub 链接占位</el-link>
    </div>
  </section>
</template>
