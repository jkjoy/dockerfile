const statusText = document.getElementById("statusText");
const refreshButton = document.getElementById("refreshButton");
const warningBanner = document.getElementById("warningBanner");
const historySearchInput = document.getElementById("historySearchInput");
const pageTabs = Array.from(document.querySelectorAll(".page-tab"));
const pageViews = Array.from(document.querySelectorAll(".page-view"));

const legacyPageMap = new Map([
  ["heroSection", "dashboard"],
  ["overviewSection", "dashboard"],
  ["predictionSection", "prediction"],
  ["performanceSection", "backtest"],
  ["statsSection", "stats"],
  ["historySection", "history"],
  ["notesSection", "notes"],
]);

const pageRoutes = {
  dashboard: "/",
  prediction: "/prediction",
  backtest: "/backtest",
  stats: "/stats",
  history: "/history",
  notes: "/notes",
};

const routeToPage = new Map(Object.entries(pageRoutes).map(([page, route]) => [route, page]));

let currentReport = null;

function setStatus(text) {
  statusText.textContent = text;
}

function formatPercent(value, digits = 2) {
  return `${Number(value).toFixed(digits)}%`;
}

function formatTinyPercent(value) {
  return `${Number(value).toFixed(8)}%`;
}

function formatNumber(value) {
  return new Intl.NumberFormat("zh-CN").format(value);
}

function formatDateTimeText(value) {
  return value ? String(value).replace("T", " ") : "--";
}

function sortNumberStrings(numbers = []) {
  return [...numbers].sort((a, b) => Number(a) - Number(b));
}

function numberBalls(numbers, color, options = {}) {
  const matchedNumbers = new Set((options.matchedNumbers || []).map(String));
  const rowClass = options.compact ? "number-row compact-row" : "number-row";
  return `
    <div class="${rowClass}">
      ${numbers
        .map((number) => {
          const text = String(number);
          const isMatched = matchedNumbers.has(text);
          return `<span class="ball ${color}${isMatched ? " matched" : ""}">${text}</span>`;
        })
        .join("")}
    </div>
  `;
}

function createBar(width, blue = false) {
  return `
    <div class="bar-track inline-bar">
      <span class="bar-fill ${blue ? "blue-fill" : ""}" style="width:${Math.max(4, width)}%"></span>
    </div>
  `;
}

function getPageFromLocation() {
  const pathname = window.location.pathname.replace(/\/+$/, "") || "/";
  if (routeToPage.has(pathname)) {
    return routeToPage.get(pathname);
  }

  const hash = window.location.hash.replace("#", "");
  if (!hash) {
    return "dashboard";
  }
  return legacyPageMap.get(hash) || hash;
}

function showPage(pageName, shouldUpdateUrl = false) {
  const target = pageViews.find((page) => page.dataset.page === pageName) || pageViews[0];
  const resolvedPage = target.dataset.page;

  pageViews.forEach((page) => {
    const isActive = page === target;
    page.classList.toggle("active-page", isActive);
    page.hidden = !isActive;
  });

  pageTabs.forEach((tab) => {
    const isActive = tab.dataset.page === resolvedPage;
    tab.classList.toggle("active", isActive);
    tab.setAttribute("aria-selected", String(isActive));
  });

  const targetRoute = pageRoutes[resolvedPage] || "/";
  if (shouldUpdateUrl && window.location.pathname !== targetRoute) {
    window.history.pushState(null, "", targetRoute);
  }

  window.scrollTo({ top: 0, behavior: "smooth" });
}

function setupPageNav() {
  pageTabs.forEach((tab) => {
    tab.addEventListener("click", () => {
      showPage(tab.dataset.page, true);
    });
  });

  window.addEventListener("popstate", () => {
    showPage(getPageFromLocation(), false);
  });

  const initialPage = getPageFromLocation();
  showPage(initialPage, false);
  const canonicalPath = pageRoutes[initialPage] || "/";
  if (window.location.pathname !== canonicalPath) {
    window.history.replaceState(null, "", canonicalPath);
  }
}

function renderSummary(report) {
  const automation = report.automation || {};
  const lastSync = automation.last_sync;
  const latestPrediction = automation.latest_prediction;
  const performance = automation.prediction_performance;

  document.getElementById("latestIssueValue").textContent = report.summary.latest_issue;
  document.getElementById("drawCountValue").textContent = formatNumber(report.summary.draw_count);
  document.getElementById("lastSyncValue").textContent = formatDateTimeText(lastSync?.finished_at || report.generated_at);
  document.getElementById("predictionBaseValue").textContent = latestPrediction?.base_issue || "--";

  const cards = [
    {
      label: "完全相同号码",
      value: report.summary.exact_duplicate_count,
      subtext:
        report.summary.exact_duplicate_count === 0
          ? "历史数据中未发现完全相同的红蓝组合。"
          : "存在完全重复组合，请查看说明页。",
    },
    {
      label: "仅红球相同",
      value: report.summary.red_only_duplicate_count,
      subtext:
        report.summary.red_only_duplicate_count === 0
          ? "未发现重复的红球六连号组合。"
          : "发现红球组合重复但蓝球不同的情况。",
    },
    {
      label: "最热红球",
      value: report.summary.most_frequent_red.number,
      subtext: `历史出现 ${report.summary.most_frequent_red.total_hits} 次`,
    },
    {
      label: "最冷红球",
      value: report.summary.least_frequent_red.number,
      subtext: `历史出现 ${report.summary.least_frequent_red.total_hits} 次`,
    },
    {
      label: "最热蓝球",
      value: report.summary.most_frequent_blue.number,
      subtext: `历史出现 ${report.summary.most_frequent_blue.total_hits} 次`,
    },
    {
      label: "最冷蓝球",
      value: report.summary.least_frequent_blue.number,
      subtext: `历史出现 ${report.summary.least_frequent_blue.total_hits} 次`,
    },
    {
      label: "已回测期数",
      value: performance ? formatNumber(performance.evaluated_total) : "--",
      subtext: performance
        ? `有中奖的预测期数 ${performance.issue_winning_total} 期`
        : "暂无回测数据",
    },
    {
      label: "票级中奖率",
      value: performance ? `${performance.ticket_win_rate_percent}%` : "--",
      subtext: performance
        ? `中奖票 ${performance.ticket_winning_total} / ${performance.ticket_total}`
        : "暂无回测数据",
    },
    {
      label: "自动同步",
      value: automation.auto_sync_enabled ? "已开启" : "已关闭",
      subtext: automation.auto_sync_enabled
        ? automation.schedule_description
        : "当前仅支持手动刷新同步",
    },
    {
      label: "下一次计划同步",
      value: formatDateTimeText(automation.next_regular_sync_at),
      subtext: automation.auto_sync_enabled
        ? `失败后会在次日 ${automation.retry_time_label} 再补拉一次`
        : "自动调度关闭时不计算计划任务",
    },
  ];

  document.getElementById("summaryCards").innerHTML = cards
    .map(
      (card) => `
        <div class="metric-card">
          <span class="metric-label">${card.label}</span>
          <span class="metric-value">${card.value}</span>
          <span class="metric-subtext">${card.subtext}</span>
        </div>
      `,
    )
    .join("");
}

function renderLatestAnalysis(report) {
  const latest = report.latest_issue_analysis;
  const exactProbabilityText = `1 / 17,721,088 ≈ ${formatTinyPercent(latest.strict_combo_probability_percent)}`;
  const redText = `红球六个号码完整组合概率约 ${formatTinyPercent(latest.strict_red_probability_percent)}`;
  const redChips = latest.red_breakdown
    .map(
      (item) => `
        <span class="info-chip">
          红 ${item.number}
          <span class="chip-score">历史 ${item.total_hits} 次 / 指数 ${item.prediction_index}</span>
        </span>
      `,
    )
    .join("");
  const blueChip = `
    <span class="info-chip">
      蓝 ${latest.blue_breakdown.number}
      <span class="chip-score">历史 ${latest.blue_breakdown.total_hits} 次 / 指数 ${latest.blue_breakdown.prediction_index}</span>
    </span>
  `;

  document.getElementById("latestAnalysis").innerHTML = `
    <div class="analysis-block">
      <strong>${latest.issue}期 ${latest.date}</strong>
      ${numberBalls(sortNumberStrings(latest.red_numbers), "red")}
      ${numberBalls([latest.blue_number], "blue")}
      <p>严格数学概率：${exactProbabilityText}</p>
      <p>${redText}</p>
      <p>按当前历史模型评分，这组号码落在历史分布的前 ${latest.historical_score_percentile}% 分位附近。</p>
    </div>
    <div class="analysis-block">
      <strong>号码拆分</strong>
      <div class="chip-row">${redChips}${blueChip}</div>
    </div>
  `;
}

function renderPrediction(report) {
  const prediction = report.prediction;
  const predictionMeta = prediction.meta;
  const latestPrediction = report.automation?.latest_prediction;
  const redChips = prediction.top_red_numbers
    .map(
      (item) => `
        <span class="info-chip">
          红 ${item.number}
          <span class="chip-score">指数 ${item.prediction_index} / 近30期 ${item.recent_30_hits} 次</span>
        </span>
      `,
    )
    .join("");
  const blueChips = prediction.top_blue_numbers
    .map(
      (item) => `
        <span class="info-chip">
          蓝 ${item.number}
          <span class="chip-score">指数 ${item.prediction_index} / 近30期 ${item.recent_30_hits} 次</span>
        </span>
      `,
    )
    .join("");
  const tickets = prediction.tickets
    .map(
      (ticket) => `
        <div class="ticket-card">
          <div class="ticket-card-head">
            <strong>候选 ${ticket.rank}</strong>
            <span class="score-pill">综合指数 ${ticket.score}</span>
          </div>
          ${numberBalls(sortNumberStrings(ticket.red_numbers), "red")}
          ${numberBalls([ticket.blue_number], "blue")}
          <div class="ticket-meta">${ticket.summary}</div>
        </div>
      `,
    )
    .join("");

  document.getElementById("predictionPanel").innerHTML = `
    <div class="prediction-list">
      <div class="prediction-block">
        <strong>模型说明</strong>
        <p class="mini-text">
          <span class="model-badge">${predictionMeta?.uses_randomness ? "含随机" : "零随机模型"}</span>
          ${predictionMeta?.ticket_rule || ""}
        </p>
        <p class="mini-text">${predictionMeta?.selection_rule || ""}</p>
      </div>
      <div class="prediction-block">
        <strong>自动预测快照</strong>
        <p class="mini-text">
          ${
            latestPrediction
              ? `当前数据库中最新预测基于 ${latestPrediction.base_issue} 期生成，生成时间 ${formatDateTimeText(latestPrediction.generated_at)}，模型版本 ${latestPrediction.model_version}。`
              : "当前还没有预测快照，系统会在首次同步后自动生成。"
          }
        </p>
        <p class="mini-text">当前 5 注号码全部按历史数据模型确定性计算生成，没有使用随机选号。</p>
      </div>
      <div class="prediction-block">
        <strong>优先关注红球</strong>
        <div class="chip-row">${redChips}</div>
      </div>
      <div class="prediction-block">
        <strong>优先关注蓝球</strong>
        <div class="chip-row">${blueChips}</div>
      </div>
      <div class="prediction-block">
        <strong>候选号码组合</strong>
        <div class="ticket-list">${tickets || "<p>暂无候选结果。</p>"}</div>
      </div>
    </div>
  `;
}

function renderPerformance(report) {
  const performance = report.automation?.prediction_performance;
  const backfill = report.automation?.history_backfill;
  if (!performance) {
    document.getElementById("performancePanel").innerHTML = `
      <div class="prediction-block"><p class="mini-text">暂无回测数据。</p></div>
    `;
    return;
  }

  const prizeBreakdown = Object.entries(performance.prize_breakdown_total)
    .map(
      ([prize, count]) => `
        <span class="info-chip">
          ${prize}
          <span class="chip-score">${count} 次</span>
        </span>
      `,
    )
    .join("");

  document.getElementById("performancePanel").innerHTML = `
    <div class="prediction-list">
      <div class="performance-hero">
        <div>
          <span class="metric-label">期级中奖率</span>
          <strong>${performance.issue_win_rate_percent}%</strong>
          <span class="metric-subtext">中奖期数 ${performance.issue_winning_total} / ${performance.evaluated_total}</span>
        </div>
        <div>
          <span class="metric-label">票级中奖率</span>
          <strong>${performance.ticket_win_rate_percent}%</strong>
          <span class="metric-subtext">中奖票 ${performance.ticket_winning_total} / ${performance.ticket_total}</span>
        </div>
      </div>
      <div class="prediction-block">
        <strong>整体表现</strong>
        <div class="chip-row">
          <span class="info-chip">回测期数 <span class="chip-score">${performance.evaluated_total}</span></span>
          <span class="info-chip">最佳奖级 <span class="chip-score">${performance.best_prize_level || "未中奖"}</span></span>
          <span class="info-chip">待开奖 <span class="chip-score">${performance.pending_total}</span></span>
        </div>
      </div>
      <div class="prediction-block">
        <strong>奖级分布</strong>
        <div class="chip-row">${prizeBreakdown}</div>
      </div>
      <div class="prediction-block">
        <strong>历史回补状态</strong>
        <p class="mini-text">
          已生成预测快照 ${performance.snapshot_total} 条，已完成验票 ${performance.evaluated_total} 条，待开奖 ${performance.pending_total} 条。
          ${backfill ? `本次回补新增快照 ${backfill.snapshot_created} 条，新增验票 ${backfill.evaluation_created} 条。` : ""}
        </p>
      </div>
    </div>
  `;
}

function renderEvaluationTicket(ticket, evaluation) {
  const actualRedNumbers = evaluation.actual_red_numbers || [];
  const actualBlueNumber = evaluation.actual_blue_number;
  const prizeText = ticket.prize_level || "未中奖";
  const blueText = ticket.blue_match ? "蓝球命中" : "蓝球未中";
  const winnerClass = ticket.is_winner ? " winning-ticket" : "";
  const prizeClass = ticket.is_winner ? " hit" : "";

  return `
    <div class="evaluation-ticket${winnerClass}">
      <div class="evaluation-ticket-head">
        <strong>第 ${ticket.rank} 注</strong>
        <span class="prize-label${prizeClass}">${prizeText}</span>
      </div>
      <div class="ticket-number-grid">
        <div>
          <span class="mini-label">预测红球</span>
          ${numberBalls(sortNumberStrings(ticket.red_numbers), "red", {
            compact: true,
            matchedNumbers: actualRedNumbers,
          })}
        </div>
        <div>
          <span class="mini-label">预测蓝球</span>
          ${numberBalls([ticket.blue_number], "blue", {
            compact: true,
            matchedNumbers: actualBlueNumber ? [actualBlueNumber] : [],
          })}
        </div>
      </div>
      <div class="ticket-meta">
        红球命中 ${ticket.red_match_count ?? 0}/6，${blueText}，综合指数 ${ticket.score ?? "--"}。
      </div>
    </div>
  `;
}

function renderEvaluations(report) {
  const performance = report.automation?.prediction_performance;
  const evaluations = performance?.recent_evaluations || [];
  if (!evaluations.length) {
    document.getElementById("evaluationPanel").innerHTML = `
      <div class="prediction-block"><p class="mini-text">暂无已开奖的预测可供验票。</p></div>
    `;
    return;
  }

  document.getElementById("evaluationPanel").innerHTML = evaluations
    .slice(0, 12)
    .map((item) => {
      const tickets = item.ticket_results || [];
      const hasWin = item.winning_ticket_count > 0;
      return `
        <article class="evaluation-card${hasWin ? " has-win" : ""}">
          <div class="evaluation-header">
            <div>
              <strong>预测基准 ${item.base_issue} -> 开奖 ${item.target_issue}</strong>
              <p class="ticket-meta">开奖日期 ${item.target_date}，模型版本 ${item.model_version}。</p>
            </div>
            <span class="result-badge${hasWin ? " hit" : ""}">${item.highest_prize_level || "未中奖"}</span>
          </div>

          <div class="draw-compare">
            <div>
              <span class="mini-label">实际开奖号码</span>
              ${numberBalls(sortNumberStrings(item.actual_red_numbers), "red")}
              ${numberBalls([item.actual_blue_number], "blue")}
            </div>
            <div class="evaluation-summary">
              <span class="info-chip">中奖票 <span class="chip-score">${item.winning_ticket_count}/${item.ticket_count}</span></span>
              <span class="info-chip">票级中奖率 <span class="chip-score">${item.winning_ticket_rate}%</span></span>
              <span class="info-chip">验票时间 <span class="chip-score">${formatDateTimeText(item.evaluated_at)}</span></span>
            </div>
          </div>

          <div class="evaluation-tickets">
            <div class="subsection-title">往期预测 5 注号码</div>
            ${
              tickets.length
                ? tickets.map((ticket) => renderEvaluationTicket(ticket, item)).join("")
                : '<p class="mini-text">暂无预测号码明细。</p>'
            }
          </div>
        </article>
      `;
    })
    .join("");
}

function renderHotCold(report) {
  const sections = [
    { title: "红球热号", items: report.hot_cold.red_hot, blue: false },
    { title: "红球冷号", items: [...report.hot_cold.red_cold].reverse(), blue: false },
    { title: "蓝球热号", items: report.hot_cold.blue_hot, blue: true },
    { title: "蓝球冷号", items: [...report.hot_cold.blue_cold].reverse(), blue: true },
  ];

  document.getElementById("hotColdPanel").innerHTML = sections
    .map(
      (section) => `
        <div class="hot-cold-block">
          <strong>${section.title}</strong>
          <div class="stat-list">
            ${section.items
              .map(
                (item) => `
                  <div class="stat-item">
                    <span class="pill">${item.number}</span>
                    ${createBar(item.prediction_index, section.blue)}
                    <span>${item.prediction_index}</span>
                  </div>
                `,
              )
              .join("")}
          </div>
        </div>
      `,
    )
    .join("");
}

function renderStatsTable(containerId, rows, blue = false) {
  const maxHits = Math.max(...rows.map((row) => row.total_hits));
  const html = `
    <div class="stats-table-wrapper">
      <table>
        <thead>
          <tr>
            <th>号码</th>
            <th>总次数</th>
            <th>历史频率</th>
            <th>近30期</th>
            <th>近60期</th>
            <th>遗漏</th>
            <th>预测指数</th>
            <th>热度</th>
          </tr>
        </thead>
        <tbody>
          ${rows
            .map(
              (row) => `
                <tr>
                  <td><span class="pill">${row.number}</span></td>
                  <td>
                    <div>${row.total_hits}</div>
                    ${createBar((row.total_hits / maxHits) * 100, blue)}
                  </td>
                  <td>${formatPercent(row.historical_rate_percent)}</td>
                  <td>${row.recent_30_hits}</td>
                  <td>${row.recent_60_hits}</td>
                  <td>${row.omission}</td>
                  <td>${row.prediction_index}</td>
                  <td>${row.heat_level}</td>
                </tr>
              `,
            )
            .join("")}
        </tbody>
      </table>
    </div>
  `;
  document.getElementById(containerId).innerHTML = html;
}

function renderHistoryTable(draws) {
  const limited = draws.slice(0, 200);
  const html = `
    <div class="stats-table-wrapper">
      <table>
        <thead>
          <tr>
            <th>期号</th>
            <th>日期</th>
            <th>红球</th>
            <th>蓝球</th>
          </tr>
        </thead>
        <tbody>
          ${limited
            .map(
              (draw) => `
                <tr>
                  <td>${draw.issue}</td>
                  <td>${draw.date}</td>
                  <td>${draw.red_display}</td>
                  <td>${draw.blue_display}</td>
                </tr>
              `,
            )
            .join("")}
        </tbody>
      </table>
    </div>
  `;
  document.getElementById("historyTable").innerHTML = html;
}

function renderNotes(report) {
  const lastSync = report.automation?.last_sync;
  const performance = report.automation?.prediction_performance;
  const exactDuplicates = report.duplicates.exact_duplicates.length
    ? report.duplicates.exact_duplicates
        .map(
          (item) =>
            `${item.red} + ${item.blue}，出现 ${item.count} 次：${item.issues
              .map((issue) => `${issue.issue}(${issue.date})`)
              .join("、")}`,
        )
        .join("；")
    : "历史数据中未发现完全相同的红蓝组合。";

  const redOnlyDuplicates = report.duplicates.red_only_duplicates.length
    ? report.duplicates.red_only_duplicates
        .map(
          (item) =>
            `${item.red} 出现 ${item.count} 次：${item.issues
              .map((issue) => `${issue.issue}(${issue.date})蓝${issue.blue}`)
              .join("、")}`,
        )
        .join("；")
    : "未发现红球六个号码完全相同但蓝球不同的重复记录。";

  document.getElementById("notesPanel").innerHTML = `
    <ul>
      ${report.notes.map((note) => `<li>${note}</li>`).join("")}
      <li>自动同步：${report.automation.auto_sync_enabled ? `开启，${report.automation.schedule_description}。下一次计划同步时间：${formatDateTimeText(report.automation.next_regular_sync_at)}。` : "关闭。当前只会在手动刷新时同步。"} </li>
      <li>最近一次同步：${lastSync ? `${formatDateTimeText(lastSync.finished_at)}，触发方式 ${lastSync.trigger_type}，模式 ${lastSync.sync_mode}，新增 ${lastSync.inserted_count} 条，更新 ${lastSync.updated_count} 条。` : "暂无同步记录。"}</li>
      <li>预测回测：${performance ? `已回测 ${performance.evaluated_total} 期，期级中奖率 ${performance.issue_win_rate_percent}%，票级中奖率 ${performance.ticket_win_rate_percent}%，最佳奖级 ${performance.best_prize_level || "未中奖"}。` : "暂无回测数据。"}</li>
      <li>完全重复检查：${exactDuplicates}</li>
      <li>红球重复检查：${redOnlyDuplicates}</li>
      <li>官方开奖页：<a class="source-link" target="_blank" rel="noreferrer" href="${report.sources.official_page_url}">${report.sources.official_page_url}</a></li>
      <li>官方接口：<a class="source-link" target="_blank" rel="noreferrer" href="${report.sources.official_api_url}">${report.sources.official_api_url}</a></li>
    </ul>
  `;
}

function applyHistoryFilter() {
  if (!currentReport) {
    return;
  }
  const keyword = historySearchInput.value.trim();
  if (!keyword) {
    renderHistoryTable(currentReport.draws);
    return;
  }
  const filtered = currentReport.draws.filter((draw) => {
    const searchBase = `${draw.issue} ${draw.date} ${draw.red_display} ${draw.blue_display}`;
    return searchBase.includes(keyword);
  });
  renderHistoryTable(filtered);
}

async function loadReport(forceRefresh = false) {
  refreshButton.disabled = true;
  setStatus(forceRefresh ? "正在刷新官方数据..." : "正在加载数据...");
  try {
    const response = await fetch(`/api/report${forceRefresh ? "?refresh=1" : ""}`, {
      cache: "no-store",
    });
    const payload = await response.json();
    if (!response.ok) {
      throw new Error(payload.error || "请求失败");
    }
    currentReport = payload;
    renderSummary(payload);
    renderLatestAnalysis(payload);
    renderPrediction(payload);
    renderPerformance(payload);
    renderEvaluations(payload);
    renderHotCold(payload);
    renderStatsTable("redStatsTable", [...payload.red_stats].sort((a, b) => b.total_hits - a.total_hits));
    renderStatsTable("blueStatsTable", [...payload.blue_stats].sort((a, b) => b.total_hits - a.total_hits), true);
    renderHistoryTable(payload.draws);
    renderNotes(payload);

    if (payload.cache_warning) {
      warningBanner.classList.remove("hidden");
      warningBanner.textContent = payload.cache_warning;
    } else {
      warningBanner.classList.add("hidden");
      warningBanner.textContent = "";
    }
    if (payload.cache_status === "fresh") {
      setStatus("已加载本地缓存");
    } else if (payload.cache_status === "database") {
      setStatus("已从本地数据库加载");
    } else if (payload.cache_status === "stale_db") {
      setStatus("官方同步失败，已回退到本地数据库");
    } else if (payload.cache_status === "synced") {
      setStatus("已同步官网并更新数据库");
    } else if (payload.cache_status === "stale_fallback") {
      setStatus("官方接口失败，已回退到本地缓存");
    } else {
      setStatus("已加载官方最新数据");
    }
  } catch (error) {
    warningBanner.classList.remove("hidden");
    warningBanner.textContent = `加载失败：${error.message}`;
    setStatus("加载失败");
  } finally {
    refreshButton.disabled = false;
  }
}

refreshButton.addEventListener("click", () => {
  loadReport(true);
});

if (historySearchInput) {
  historySearchInput.addEventListener("input", () => {
    applyHistoryFilter();
  });
}

setupPageNav();
loadReport(false);
