#!groovy

milestone 0
timestamps {
    node('docker') {
        def commitHash = checkout(scm).GIT_COMMIT

        docker.withRegistry('https://harbor.cyverse.org', 'jenkins-harbor-credentials') {
            def dockerImage
            stage('Build') {
                milestone 50
                try {
                    dockerImage = docker.build("harbor.cyverse.org/de/vice-file-transfers:${env.BUILD_TAG}", "--build-arg porklock_tag=${env.BRANCH_NAME} --build-arg git_commit=${commitHash} .")
                } catch ( e ) {
                    dockerImage = docker.build("harbor.cyverse.org/de/vice-file-transfers:${env.BUILD_TAG}", "--build-arg git_commit=${commitHash} .")
                }
                milestone 51
                dockerImage.push()
            }
            stage('Test') {
                dockerTestRunner = "test-${env.BUILD_TAG}"
                try {
                    sh "docker create --name ${dockerTestRunner} ${dockerImage.imageName()}"
                    sh "docker cp ${dockerTestRunner}:/test-results.xml ."
                    sh "touch test-results.xml" // whatever
                    sh "docker rm ${dockerTestRunner}"
                } finally {
                    junit 'test-results.xml'

                    sh "docker run --rm -v \$(pwd):/build -w /build alpine rm -r test-results.xml"
                }
            }
            stage('Docker Push') {
                milestone 100
                dockerImage.push("${env.BRANCH_NAME}")
                // Retag to 'qa' if this is master/main (keep both so when it switches this keeps working)
                if ( "${env.BRANCH_NAME}" == "master" || "${env.BRANCH_NAME}" == "main" ) {
                    dockerImage.push("qa")
                }
                milestone 101
            }
        }
    }
}
