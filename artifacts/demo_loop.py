#!/usr/bin/python3
import time
import requests
import random
import subprocess
import json
import sys

import argparse

# Short questions (existing)
QUESTIONS = {
    "Science": [
        "Explain the significance of the Higgs Boson in particle physics.",
        "How do CRISPR-Cas9 gene editing tools work?",
        "What is the current understanding of dark matter?",
        "Explain the process of photosynthesis at the molecular level.",
        "What are the latest findings from the James Webb Space Telescope?",
        "Solve this complex calculus problem involving partial derivatives.",
        "Explain the principles of quantum entanglement."
    ],
    "Finance": [
        "What are the implications of a yield curve inversion for the economy?",
        "How does decentralized finance (DeFi) differ from traditional banking?",
        "Explain the concept of 'Short Squeeze' in the stock market.",
        "What is the impact of quantitative easing on inflation?",
        "How do central banks use interest rates to control economic growth?",
        "What are the key differences between macroeconomic and microeconomic theories?",
        "Explain the stages of a standard business cycle."
    ],
    "Technology": [
        "How does a Zero-Knowledge Proof work in blockchain security?",
        "Explain the architectural differences between Monoliths and Microservices.",
        "What are the best practices for securing a Kubernetes cluster?",
        "How do transformer models like GPT-4 process sequential data?",
        "What is the future of WebAssembly in cloud-native development?",
        "What are the fundamental principles of structural engineering?",
        "Explain the difference between TCP and UDP protocols."
    ],
    "Health": [
        "What are the psychological effects of long-term social isolation?",
        "Explain the role of the microbiome in human immune function.",
        "How do mRNA vaccines differ from traditional viral vector vaccines?",
        "What are the latest breakthroughs in Alzheimer's research?",
        "How does circadian rhythm affect overall metabolic health?",
        "What are the diagnostic criteria for widespread generalized anxiety disorder?",
        "Explain the mechanism of action of SSRI antidepressants."
    ],
    "Legal": [
        "What are the legal challenges of AI-generated intellectual property?",
        "Explain the difference between Common Law and Civil Law systems.",
        "How does the General Data Protection Regulation (GDPR) affect non-EU companies?",
        "What is the impact of the 'Right to be Forgotten' on search engines?",
        "Explain the legal doctrine of 'Fair Use' in the digital age.",
        "What were the primary causes of the fall of the Western Roman Empire?",
        "Discuss the historical significance of the Magna Carta."
    ],
    "General": [
        "What are the main arguments for and against utilitarianism in moral philosophy?",
        "Explain the concept of existentialism as proposed by Jean-Paul Sartre.",
        "What is the meaning and origin of the proverb 'A stitch in time saves nine'?",
        "Discuss the cultural impact of the Renaissance period on modern society.",
        "Explain Plato's Allegory of the Cave and its epistemological implications."
    ]
}

# Long questions (>500 tokens roughly) to trigger budget pressure
LONG_QUESTIONS = {
    "Science": [
        "Provide a comprehensive analysis of the thermal evolution of the early universe, specifically detailing the epoch of recombination and how the subsequent decoupling of photons led to the formation of the Cosmic Microwave Background radiation. Please include the roles of baryonic acoustic oscillations and the impact of dark energy on the current rate of expansion as measured by the Hubble tension between Planck and SH0ES data.",
        "Detail the molecular mechanics of DNA replication in eukaryotic cells, focusing on the specific roles of helicase, primase, DNA polymerase delta and epsilon, and the coordination of the replication fork. Explain the 'end replication problem' and how telomerase functions as a reverse transcriptase to maintain chromosomal stability through repeated TTAGGG repeats during cellular senescence."
    ],
    "Finance": [
        "Draft a detailed investment thesis for the transition from centralized fiat banking systems to decentralized algorithmic stablecoin protocols. Discuss the systemic risks associated with collateralized debt positions (CDPs), the historical precedent of the trilemma between capital mobility, fixed exchange rates, and autonomous monetary policy, and how layer-2 scaling solutions like ZK-Rollups impact the total value locked (TVL) in the ecosystem.",
        "Analyze the impact of the Basel III framework on global liquidity ratios, specifically focusing on the Liquidity Coverage Ratio (LCR) and the Net Stable Funding Ratio (NSFR). How have these regulations influenced the risk-weighting of sovereign debt vs. corporate assets among G-SIBs (Global Systemically Important Banks) since the 2008 financial crisis, and what are the implications for credit availability in emerging markets?"
    ],
    "Technology": [
        "Compare and contrast the Byzantine Fault Tolerance algorithms used in major decentralized consensus networks, specifically focusing on Practical Byzantine Fault Tolerance (pBFT) versus Nakamoto Consensus. Elaborate on how these differences impact network latency, finality time, and susceptibility to 51% attacks, and how modern Proof of Stake mechanisms attempt to solve the blockchain trilemma of scalability, security, and decentralization.",
        "Provide an in-depth technical analysis surrounding the migration of a legacy monolithic enterprise application, currently running on on-premise bare-metal servers, to a microservices architecture on a managed Kubernetes cluster. Evaluate the trade-offs between utilizing an API gateway pattern with an Envoy-based service mesh (such as Istio) versus using basic Ingress controllers, specifically focusing on mutual TLS (mTLS) enforcement, distributed tracing integration, and canary deployment strategies."
    ],
    "Health": [
        "Evaluate the complex interplay between the gut-brain axis and neurodegenerative diseases such as Parkinson's and Alzheimer's. Describe the proposed pathogenic mechanisms by which dysbiosis of the intestinal microbiome might influence neuroinflammation, the role of short-chain fatty acids (SCFAs) passing the blood-brain barrier, and the potential for leveraging fecal microbiota transplantation (FMT) as an adjunctive therapeutic intervention.",
        "Discuss the psychological and neurological implications of chronic exposure to unpredictable social stressors. Focus on the disruption of the hypothalamic-pituitary-adrenal (HPA) axis, the resulting glucocorticoid receptor resistance, its correlation with major depressive disorder, and how cognitive behavioral therapy (CBT) combined with pharmacological interventions aims to restore allostatic load balance."
    ],
    "Legal": [
        "Synthesize the legal arguments surrounding the extraterritorial application of the CLOUD Act (Clarifying Lawful Overseas Use of Data Act) in contrast with the EU's GDPR Article 48 regarding the transfer of personal data to third-party law enforcement. Analyze the specific challenges for multi-national CSPs (Cloud Service Providers) in resolving conflict-of-law scenarios where a US warrant conflicts with a European blocking statute.",
        "Examine the evolution of the 'Transformative Use' doctrine in United States copyright law, tracing the development from Campbell v. Acuff-Rose Music, Inc. through to the recent Supreme Court decision in Andy Warhol Foundation for the Visual Arts, Inc. v. Goldsmith. Discuss how these precedents establish a framework for the licensing of training data in Generative AI models."
    ],
    "General": [
        "Critically evaluate the metaphysical foundations of determinism versus free will, contrasting the compatibilist perspective associated with David Hume and modern cognitive science against the incompatibilist views of strict determinism and libertarian free will. How do recent neuroscientific discoveries, such as the Libet experiment regarding the readiness potential in the supplementary motor area, challenge traditional ethical frameworks surrounding moral culpability and the justification of retributive justice?",
        "Perform a comprehensive historiographical analysis of the socio-economic factors that precipitated the French Revolution, going beyond the traditional narrative of the Enlightenment and the opulence of the monarchy. Investigate the impact of the deregulation of the grain market (the Flour War), the regressive taxation system (the taille), the catastrophic harvests of the late 1780s, and how these compounding crises catalyzed the transition from the Estates-General to the radicalization of the Jacobin Club during the Reign of Terror."
    ]
}

def get_gateway_address():
    """Retrieves the LoadBalancer address for the AgentGateway."""
    try:
        cmd = "kubectl get svc -n agentgateway-system agentgateway-proxy -o jsonpath=\"{.status.loadBalancer.ingress[0]['hostname','ip']}\""
        address = subprocess.check_output(cmd, shell=True, text=True, stderr=subprocess.DEVNULL).strip()
        return address if address else "localhost:8080"
    except:
        return "localhost:8080"

def get_pricing():
    """Retrieves pricing info for all Evaluated models from LLMBackend CRs."""
    pricing = {}
    try:
        cmd = "kubectl get llmbackends -A -o json"
        data = json.loads(subprocess.check_output(cmd, shell=True, text=True))
        for item in data.get("items", []):
            status = item.get("status", {})
            if status.get("phase") == "Evaluated":
                model_name = item.get("spec", {}).get("model")
                p = status.get("results", {}).get("pricing", {})
                if model_name:
                    pricing[model_name] = {
                        "prompt": float(p.get("prompt", 0)),
                        "completion": float(p.get("completion", 0))
                    }
    except Exception as e:
        print(f"Warning: Could not fetch pricing: {e}")
    return pricing

def get_budget_pressure():
    """Retrieves the latest budget pressure from the router logs."""
    try:
        cmd = "kubectl logs -n intelligent-router-system intelligent-router-0 --tail=20 | grep \"budget_pressure=\" | tail -n 1"
        line = subprocess.check_output(cmd, shell=True, text=True, stderr=subprocess.DEVNULL).strip()
        if "budget_pressure=" in line:
            parts = line.split("budget_pressure=")
            return parts[1].split()[0]
        return "0.000"
    except:
        return "0.000"

def get_current_strategy():
    """Retrieves the current OPTIMIZATION_TARGET from the live environment."""
    try:
        cmd = "kubectl get po -n intelligent-router-system intelligent-router-0 -o jsonpath='{.spec.containers[0].env[?(@.name==\"OPTIMIZATION_TARGET\")].value}'"
        result = subprocess.check_output(cmd, shell=True, text=True, stderr=subprocess.DEVNULL).strip()
        return result if result else "accuracy"
    except:
        return "accuracy"

def run_demo(pool_type="short", show_pressure=False):
    gateway_address = get_gateway_address()
    base_url = f"http://{gateway_address}/v1/chat/completions"
    pricing_map = get_pricing()
    
    questions_pool = LONG_QUESTIONS if pool_type == "long" else QUESTIONS
    title_suffix = " (LONG PROMPTS MODE)" if pool_type == "long" else ""
    
    print("\n" + "═"*145)
    print(f" 🚀 INTELLIGENT ROUTER LIVE DEMO LOOP{title_suffix}")
    print(f" 📡 Monitoring: {base_url}")
    print("═" * 145 + "\n")
    
    # Conditionally format header
    if show_pressure:
        header = f"{'DOMAIN':<12} │ {'STRATEGY':<10} │ {'PRESSURE':<10} │ {'LATENCY':<10} │ {'TOK/S':<10} │ {'COST (€)':<10} │ {'SELECTED MODEL':<23} │ {'PROMPT'}"
    else:
        header = f"{'DOMAIN':<12} │ {'STRATEGY':<10} │ {'LATENCY':<10} │ {'TOK/S':<10} │ {'COST (€)':<10} │ {'SELECTED MODEL':<23} │ {'PROMPT'}"
    print(header)
    print("─" * 145)

    all_domains = list(questions_pool.keys())
    
    try:
        while True:
            domain = random.choice(all_domains)
            prompt = random.choice(questions_pool[domain])
            strategy = get_current_strategy()
            
            payload = {
                "model": "auto",
                "messages": [{"role": "user", "content": prompt}],
                "max_completion_tokens": 500
            }
            
            start_time = time.time()
            try:
                response = requests.post(
                    base_url,
                    json=payload,
                    timeout=60
                )
                duration = time.time() - start_time
                latency_str = f"{duration:.2f}s"
                
                pressure = "0.000"
                if show_pressure:
                    pressure = get_budget_pressure()
                
                if response.status_code == 200:
                    data = response.json()
                    model = data.get("model", "unknown")
                    usage = data.get("usage", {})
                    prompt_tokens = usage.get("prompt_tokens", 0)
                    completion_tokens = usage.get("completion_tokens", 0)
                    
                    tps = completion_tokens / duration if duration > 0 else 0
                    tps_str = f"{tps:.1f}"
                    
                    cost = 0.0
                    clean_model = model.split("/")[-1] if "/" in model else model
                    matched_price = None
                    for m_name, p in pricing_map.items():
                        if m_name in clean_model:
                            matched_price = p
                            break
                    
                    if matched_price:
                        cost = (prompt_tokens * matched_price["prompt"] / 1_000_000) + \
                               (completion_tokens * matched_price["completion"] / 1_000_000)
                    
                    cost_str = f"€{cost:.5f}" if cost > 0 else "-"
                    
                    if show_pressure:
                        p_val = float(pressure)
                        p_display = f"\033[93m{pressure}\033[0m" if p_val > 0 else pressure
                        print(f"{domain:<12} │ {strategy[:10]:<10} │ {p_display:<19} │ {latency_str:<10} │ {tps_str:<10} │ {cost_str:<10} │ {clean_model:<23} │ {prompt[:40]}...")
                    else:
                        print(f"{domain:<12} │ {strategy[:10]:<10} │ {latency_str:<10} │ {tps_str:<10} │ {cost_str:<10} │ {clean_model:<23} │ {prompt[:40]}...")
                else:
                    if show_pressure:
                        print(f"{domain:<12} │ {strategy[:10]:<10} │ {pressure:<10} │ {latency_str:<10} │ {'-':<10} │ {'-':<10} │ ⚠️ ERROR {response.status_code:<16} │ {prompt[:40]}...")
                    else:
                        print(f"{domain:<12} │ {strategy[:10]:<10} │ {latency_str:<10} │ {'-':<10} │ {'-':<10} │ ⚠️ ERROR {response.status_code:<16} │ {prompt[:40]}...")
            except requests.exceptions.ConnectionError:
                if show_pressure:
                    print(f"{domain:<12} │ {strategy[:10]:<10} │ {'-':<10} │ {'-':<10} │ {'-':<10} │ {'-':<10} │ 🔌 OFFLINE              │ Check gateway at {gateway_address}")
                else:
                    print(f"{domain:<12} │ {strategy[:10]:<10} │ {'-':<10} │ {'-':<10} │ {'-':<10} │ 🔌 OFFLINE                 │ Check gateway at {gateway_address}")
            except Exception as e:
                if show_pressure:
                    print(f"{domain:<12} │ {strategy[:10]:<10} │ {'-':<10} │ {'-':<10} │ {'-':<10} │ {'-':<10} │ ❌ FAILED               │ {str(e)[:30]}...")
                else:
                    print(f"{domain:<12} │ {strategy[:10]:<10} │ {'-':<10} │ {'-':<10} │ {'-':<10} │ ❌ FAILED                  │ {str(e)[:30]}...")
            
            time.sleep(1.0)
            
    except KeyboardInterrupt:
        print("\n" + "═"*145)
        print(" 👋 Demo loop stopped. Happy recording!")
        print("═"*145)

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Intelligent Router Demo Loop")
    parser.add_argument("--long", action="store_true", help="Use long prompts to trigger budget pressure")
    parser.add_argument("--pressure", action="store_true", help="Show token budget pressure column")
    args = parser.parse_args()

    # Ensure dependencies are available
    try:
        import requests
    except ImportError:
        print("Error: 'requests' library not found. Please run: pip install requests")
        sys.exit(1)
        
    run_demo(pool_type="long" if args.long else "short", show_pressure=args.pressure)
