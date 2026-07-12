import React from 'react'
import { useI18n } from '../i18n'
import LanguageToggle from './LanguageToggle'

const LandingPage = ({ onEnter }) => {
    const { t } = useI18n()
    return (
        <div className="landing-container min-h-screen relative overflow-hidden flex flex-col items-center justify-center p-6 text-center">
            {/* Animated Background Elements - using Tailwind with some custom CSS in styles.css if needed, 
                but for simplicity I will use inline styles to match the backend version precisely */}
            <style dangerouslySetInnerHTML={{
                __html: `
                .landing-container {
                    background-color: #030712;
                    color: #f9fafb;
                    font-family: 'Inter', sans-serif;
                }
                .bg-glow {
                    position: fixed;
                    top: 0;
                    left: 0;
                    width: 100%;
                    height: 100%;
                    z-index: 0;
                    background: 
                        radial-gradient(circle at 20% 30%, rgba(245, 158, 11, 0.05) 0%, transparent 40%),
                        radial-gradient(circle at 80% 70%, rgba(239, 68, 68, 0.05) 0%, transparent 40%);
                }
                .blob {
                    position: absolute;
                    width: 400px;
                    height: 400px;
                    background: linear-gradient(135deg, #f59e0b, #ef4444);
                    filter: blur(80px);
                    opacity: 0.15;
                    border-radius: 50%;
                    z-index: 0;
                    animation: move 20s infinite alternate;
                }
                @keyframes move {
                    from { transform: translate(-10%, -10%) scale(1); }
                    to { transform: translate(10%, 10%) scale(1.1); }
                }
                .landing-content {
                    position: relative;
                    z-index: 10;
                    max-width: 900px;
                    animation: fadeInUp 0.8s ease-out;
                }
                @keyframes fadeInUp {
                    from { opacity: 0; transform: translateY(20px); }
                    to { opacity: 1; transform: translateY(0); }
                }
                .logo-text {
                    font-family: 'Orbitron', sans-serif;
                    font-size: clamp(3rem, 10vw, 5rem);
                    font-weight: 700;
                    background: linear-gradient(135deg, #f59e0b, #ef4444);
                    -webkit-background-clip: text;
                    -webkit-text-fill-color: transparent;
                    background-clip: text;
                    letter-spacing: -2px;
                    margin-bottom: 0.5rem;
                }
                .btn-premium {
                    background: linear-gradient(135deg, #f59e0b, #ef4444);
                    box-shadow: 0 4px 15px rgba(245, 158, 11, 0.4);
                }
                .btn-premium:hover {
                    box-shadow: 0 8px 25px rgba(245, 158, 11, 0.6);
                    transform: translateY(-3px) scale(1.02);
                }
                .glass-card {
                    background: rgba(255, 255, 255, 0.03);
                    border: 1px solid rgba(255, 255, 255, 0.08);
                    backdrop-filter: blur(10px);
                    transition: all 0.3s ease;
                }
                .glass-card:hover {
                    border-color: rgba(245, 158, 11, 0.3);
                    background: rgba(255, 255, 255, 0.05);
                    transform: translateY(-5px);
                }
            `}} />

            <div className="bg-glow" />
            <div className="blob" style={{ top: '10%', left: '15%' }} />
            <div className="blob" style={{ bottom: '10%', right: '15%', animationDelay: '-5s' }} />

            <div className="absolute top-6 right-6 z-20">
                <LanguageToggle />
            </div>

            <div className="landing-content">
                <header className="mb-12">
                    <h1 className="logo-text">DS2API</h1>
                    <p className="text-gray-400 text-xl max-w-2xl mx-auto leading-relaxed">
                        DeepSeek to OpenAI & Claude Compatible API Interface
                    </p>
                </header>

                <div className="flex flex-wrap gap-4 justify-center mb-16">
                    <button
                        onClick={onEnter}
                        className="btn-premium text-white px-8 py-3 rounded-xl font-bold transition-all flex items-center gap-2"
                    >
                        {t('landing.adminConsole')}
                    </button>
                    <a
                        href="/v1/models"
                        target="_blank"
                        className="glass-card text-white px-8 py-3 rounded-xl font-semibold transition-all flex items-center gap-2"
                    >
                        {t('landing.apiStatus')}
                    </a>
                    <a
                        href="https://github.com/CJackHwang/ds2api"
                        target="_blank"
                        className="glass-card text-white px-8 py-3 rounded-xl font-semibold transition-all flex items-center gap-2"
                    >
                        GitHub
                    </a>
                </div>

                <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-6 text-left">
                    {[
                        { title: t('landing.features.compatibility.title'), desc: t('landing.features.compatibility.desc') },
                        { title: t('landing.features.loadBalancing.title'), desc: t('landing.features.loadBalancing.desc') },
                        { title: t('landing.features.reasoning.title'), desc: t('landing.features.reasoning.desc') },
                        { title: t('landing.features.search.title'), desc: t('landing.features.search.desc') },
                    ].map((feature, idx) => (
                        <div key={idx} className="glass-card p-6 rounded-2xl">
                            <h3 className="text-lg font-bold mb-2">{feature.title}</h3>
                            <p className="text-sm text-gray-400 leading-relaxed">{feature.desc}</p>
                        </div>
                    ))}
                </div>

                <footer className="mt-20 opacity-40 text-sm">
                    <p>&copy; 2026 DS2API Project. Designed for flexibility & performance.</p>
                </footer>
            </div>
        </div>
    )
}

export default LandingPage
